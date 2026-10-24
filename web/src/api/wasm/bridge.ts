import type { BackendStatus } from '../backendStatus';
import type {
  HeaderPairs,
  MainToWorkerMessage,
  WorkerRequestMessage,
  WorkerToMainMessage,
} from './protocol';

/** The parts of `Worker` the bridge needs; lets tests pass a fake. */
export interface WorkerPort {
  postMessage(message: MainToWorkerMessage, transfer: Transferable[]): void;
  addEventListener(
    type: 'message',
    listener: (event: MessageEvent<WorkerToMainMessage>) => void,
  ): void;
  addEventListener(type: 'error', listener: (event: Event) => void): void;
  /** Stops the worker at once (`Worker.terminate`). */
  terminate?(): void;
}

interface Pending {
  resolve: (response: Response) => void;
  reject: (err: unknown) => void;
}

export interface WasmBridgeOptions {
  /** Saving the demo's data failed; reported once. */
  onSaveFailed?: (message: string) => void;
}

/** Statuses for which `new Response()` must not be given a body. */
const NULL_BODY_STATUSES = new Set([101, 103, 204, 205, 304]);

/**
 * Main-thread side of the WASM transport: implements the `fetch` signature on
 * top of a worker that hosts the Go backend. Requests are correlated with
 * replies by id; bodies are transferred, not copied.
 */
export class WasmBridge {
  private nextId = 1;
  private readonly pending = new Map<number, Pending>();
  private fatalError: Error | null = null;
  private readonly port: WorkerPort;
  private readonly onStatus: (status: BackendStatus) => void;
  /** Resolves when the backend is ready, rejects if it fails to start. */
  readonly ready: Promise<void>;
  private resolveReady!: () => void;
  private rejectReady!: (err: Error) => void;
  private readonly flushes = new Map<number, () => void>();
  private stopped = false;
  private readonly options: WasmBridgeOptions;

  constructor(
    port: WorkerPort,
    onStatus: (status: BackendStatus) => void = () => undefined,
    options: WasmBridgeOptions = {},
  ) {
    this.port = port;
    this.onStatus = onStatus;
    this.options = options;
    this.ready = new Promise<void>((resolve, reject) => {
      this.resolveReady = resolve;
      this.rejectReady = reject;
    });
    // Callers await `ready` per request; do not surface an unhandled rejection.
    this.ready.catch(() => undefined);

    this.onStatus({ state: 'loading' });
    port.addEventListener('message', (event) => this.handleMessage(event.data));
    port.addEventListener('error', (event) => {
      const message = event instanceof ErrorEvent && event.message ? event.message : 'Worker error';
      this.fail(message);
    });
  }

  /** `fetch`-compatible function. Accepts a `Request` (as openapi-fetch passes) or URL + init. */
  fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const request = toRequest(input, init);
    const url = new URL(request.url);
    const method = request.method.toUpperCase();

    let body: ArrayBuffer | null = null;
    if (method !== 'GET' && method !== 'HEAD') {
      const buffer = await request.arrayBuffer();
      body = buffer.byteLength > 0 ? buffer : null;
    }

    if (request.signal.aborted) throw abortError(request.signal);
    if (this.fatalError) throw new TypeError(this.fatalError.message);
    await this.ready;

    const id = this.nextId++;
    const message: WorkerRequestMessage = {
      type: 'request',
      id,
      method,
      url: url.pathname + url.search,
      headers: [...request.headers.entries()],
      body,
    };

    return new Promise<Response>((resolve, reject) => {
      const onAbort = () => {
        this.pending.delete(id);
        reject(abortError(request.signal));
      };
      request.signal.addEventListener('abort', onAbort, { once: true });

      this.pending.set(id, {
        resolve: (response) => {
          request.signal.removeEventListener('abort', onAbort);
          resolve(response);
        },
        reject: (err) => {
          request.signal.removeEventListener('abort', onAbort);
          reject(err);
        },
      });
      this.port.postMessage(message, body ? [body] : []);
    });
  };

  /**
   * Asks the worker to save pending changes now. Resolves once they are
   * saved, or after `timeoutMs` at the latest (a crashed backend never
   * answers), so callers can always go on.
   */
  flush(timeoutMs = 2_000): Promise<void> {
    if (this.stopped) return Promise.resolve();
    const id = this.nextId++;
    return new Promise<void>((resolve) => {
      const timer = setTimeout(done, timeoutMs);
      function done() {
        clearTimeout(timer);
        resolve();
      }
      this.flushes.set(id, done);
      this.port.postMessage({ type: 'flush', id }, []);
    }).finally(() => this.flushes.delete(id));
  }

  /**
   * Stops the worker: nothing is saved any more. Requests still open fail,
   * and later ones fail at once.
   */
  terminate(): void {
    if (this.stopped) return;
    this.stopped = true;
    this.port.terminate?.();
    const err = new TypeError('The demo backend was stopped');
    this.fatalError ??= err;
    this.rejectReady(err);
    for (const pending of this.pending.values()) pending.reject(err);
    this.pending.clear();
    for (const done of this.flushes.values()) done();
  }

  private handleMessage(msg: WorkerToMainMessage): void {
    if (this.stopped) return;
    switch (msg.type) {
      case 'flushed':
        this.flushes.get(msg.id)?.();
        break;
      case 'save-failed':
        this.options.onSaveFailed?.(msg.message);
        break;
      case 'ready':
        this.resolveReady();
        this.onStatus({ state: 'ready' });
        break;
      case 'fatal':
        this.fail(msg.message);
        break;
      case 'response': {
        const pending = this.take(msg.id);
        if (!pending) return;
        try {
          pending.resolve(toResponse(msg.status, msg.statusText, msg.headers, msg.body));
        } catch (err) {
          pending.reject(err);
        }
        break;
      }
      case 'error': {
        // Like a network failure in real fetch.
        this.take(msg.id)?.reject(new TypeError(msg.message));
        break;
      }
    }
  }

  private take(id: number): Pending | undefined {
    const pending = this.pending.get(id);
    this.pending.delete(id);
    return pending;
  }

  private fail(message: string): void {
    if (this.fatalError) return;
    this.fatalError = new Error(message);
    this.rejectReady(this.fatalError);
    for (const pending of this.pending.values()) pending.reject(new TypeError(message));
    this.pending.clear();
    this.onStatus({ state: 'error', message });
  }
}

function toRequest(input: RequestInfo | URL, init?: RequestInit): Request {
  if (input instanceof Request) return init ? new Request(input, init) : input;
  // Resolve relative paths like "/api/v1/health" against the page.
  const base = globalThis.location?.href ?? 'http://localhost/';
  return new Request(new URL(String(input), base), init);
}

export function toResponse(
  status: number,
  statusText: string,
  headers: HeaderPairs,
  body: ArrayBuffer,
): Response {
  const responseHeaders = new Headers();
  for (const [name, value] of headers) responseHeaders.append(name, value);
  const hasBody = !NULL_BODY_STATUSES.has(status);
  return new Response(hasBody ? body : null, { status, statusText, headers: responseHeaders });
}

function abortError(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException('The operation was aborted.', 'AbortError');
}
