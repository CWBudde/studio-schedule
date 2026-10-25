import createClient from 'openapi-fetch';
import { describe, expect, it, vi } from 'vitest';
import type { paths } from '../schema';
import type { BackendStatus } from '../backendStatus';
import { WasmBridge, type WorkerPort } from './bridge';
import type { MainToWorkerMessage, WorkerRequestMessage, WorkerToMainMessage } from './protocol';

const encoder = new TextEncoder();

/** In-memory stand-in for the Worker: records posts, lets tests emit replies. */
class FakeWorker implements WorkerPort {
  readonly posted: { message: WorkerRequestMessage; transfer: Transferable[] }[] = [];
  readonly flushes: number[] = [];
  terminated = false;
  private readonly listeners = new Map<string, ((event: never) => void)[]>();
  /** Optional auto-responder, invoked for every posted request. */
  respond?: (message: WorkerRequestMessage) => WorkerToMainMessage | undefined;

  postMessage(message: MainToWorkerMessage, transfer: Transferable[]): void {
    if (message.type === 'flush') {
      this.flushes.push(message.id);
      return;
    }
    this.posted.push({ message, transfer });
    const reply = this.respond?.(message);
    if (reply) queueMicrotask(() => this.emit(reply));
  }

  addEventListener(type: string, listener: (event: never) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }

  emit(message: WorkerToMainMessage): void {
    for (const listener of this.listeners.get('message') ?? []) {
      listener({ data: message } as never);
    }
  }

  terminate(): void {
    this.terminated = true;
  }

  emitError(): void {
    for (const listener of this.listeners.get('error') ?? []) listener(new Event('error') as never);
  }
}

function json(body: unknown): ArrayBuffer {
  return encoder.encode(JSON.stringify(body)).buffer;
}

async function flush() {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

describe('WasmBridge', () => {
  it('reports loading, then ready', () => {
    const statuses: BackendStatus[] = [];
    const worker = new FakeWorker();
    new WasmBridge(worker, (s) => statuses.push(s));
    worker.emit({ type: 'ready' });
    expect(statuses).toEqual([{ state: 'loading' }, { state: 'ready' }]);
  });

  it('waits for ready before posting requests', async () => {
    const worker = new FakeWorker();
    const bridge = new WasmBridge(worker);
    const pending = bridge.fetch('http://localhost/api/v1/health');
    await flush();
    expect(worker.posted).toHaveLength(0);

    worker.emit({ type: 'ready' });
    await flush();
    expect(worker.posted).toHaveLength(1);

    const id = worker.posted[0]!.message.id;
    worker.emit({ type: 'response', id, status: 200, statusText: '', headers: [], body: json({}) });
    await expect(pending).resolves.toBeInstanceOf(Response);
  });

  it('converts a Request into a worker message and transfers the body', async () => {
    const worker = new FakeWorker();
    const bridge = new WasmBridge(worker);
    worker.emit({ type: 'ready' });

    const request = new Request('http://localhost/api/v1/things?limit=5', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Custom': 'yes' },
      body: JSON.stringify({ name: 'Ballet I' }),
    });
    const pending = bridge.fetch(request);
    await flush();

    const { message, transfer } = worker.posted[0]!;
    expect(message).toMatchObject({
      type: 'request',
      method: 'POST',
      url: '/api/v1/things?limit=5',
    });
    expect(message.headers).toEqual(
      expect.arrayContaining([
        ['content-type', 'application/json'],
        ['x-custom', 'yes'],
      ]),
    );
    expect(message.body).toBeInstanceOf(ArrayBuffer);
    expect(transfer).toEqual([message.body]);
    expect(new TextDecoder().decode(message.body!)).toBe('{"name":"Ballet I"}');

    worker.emit({
      type: 'response',
      id: message.id,
      status: 201,
      statusText: '',
      headers: [
        ['Content-Type', 'application/json'],
        ['X-Multi', 'a'],
        ['X-Multi', 'b'],
      ],
      body: json({ id: 42 }),
    });
    const response = await pending;
    expect(response.status).toBe(201);
    expect(response.headers.get('content-type')).toBe('application/json');
    expect(response.headers.get('x-multi')).toBe('a, b');
    await expect(response.json()).resolves.toEqual({ id: 42 });
  });

  it('resolves relative URLs and sends GET without a body', async () => {
    const worker = new FakeWorker();
    const bridge = new WasmBridge(worker);
    worker.emit({ type: 'ready' });
    void bridge.fetch('/api/v1/health?x=1');
    await flush();
    expect(worker.posted[0]?.message).toMatchObject({
      method: 'GET',
      url: '/api/v1/health?x=1',
      body: null,
    });
    expect(worker.posted[0]?.transfer).toEqual([]);
  });

  it('builds body-less responses for 204', async () => {
    const worker = new FakeWorker();
    worker.respond = (m) => ({
      type: 'response',
      id: m.id,
      status: 204,
      statusText: '',
      headers: [],
      body: new ArrayBuffer(0),
    });
    const bridge = new WasmBridge(worker);
    worker.emit({ type: 'ready' });
    const response = await bridge.fetch('http://localhost/api/v1/thing', { method: 'DELETE' });
    expect(response.status).toBe(204);
    expect(response.body).toBeNull();
  });

  it('rejects with TypeError on per-request errors', async () => {
    const worker = new FakeWorker();
    worker.respond = (m) => ({ type: 'error', id: m.id, message: 'boom' });
    const bridge = new WasmBridge(worker);
    worker.emit({ type: 'ready' });
    await expect(bridge.fetch('http://localhost/api/v1/health')).rejects.toThrow(TypeError);
  });

  it('fails pending and future requests when the backend crashes', async () => {
    const statuses: BackendStatus[] = [];
    const worker = new FakeWorker();
    const bridge = new WasmBridge(worker, (s) => statuses.push(s));
    worker.emit({ type: 'ready' });

    const pending = bridge.fetch('http://localhost/api/v1/health');
    await flush();
    worker.emit({ type: 'fatal', message: 'exit code 2' });

    await expect(pending).rejects.toThrow('exit code 2');
    await expect(bridge.fetch('http://localhost/api/v1/health')).rejects.toThrow('exit code 2');
    expect(statuses.at(-1)).toEqual({ state: 'error', message: 'exit code 2' });
  });

  it('fails startup when the worker itself errors', async () => {
    const statuses: BackendStatus[] = [];
    const worker = new FakeWorker();
    const bridge = new WasmBridge(worker, (s) => statuses.push(s));
    worker.emitError();
    await expect(bridge.ready).rejects.toThrow('Worker error');
    expect(statuses.at(-1)?.state).toBe('error');
  });

  it('supports aborting a request', async () => {
    const worker = new FakeWorker();
    const bridge = new WasmBridge(worker);
    worker.emit({ type: 'ready' });
    const controller = new AbortController();
    const pending = bridge.fetch('http://localhost/api/v1/health', { signal: controller.signal });
    await flush();
    controller.abort();
    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('flushes pending saves, and gives up waiting after a timeout', async () => {
    vi.useFakeTimers();
    try {
      const worker = new FakeWorker();
      const bridge = new WasmBridge(worker);
      worker.emit({ type: 'ready' });

      let flushed = false;
      const flush = bridge.flush(1_000).then(() => (flushed = true));
      expect(worker.flushes).toHaveLength(1);
      await vi.advanceTimersByTimeAsync(10);
      expect(flushed).toBe(false);
      worker.emit({ type: 'flushed', id: worker.flushes[0]! });
      await flush;

      // A crashed backend never answers.
      let late = false;
      const stuck = bridge.flush(1_000).then(() => (late = true));
      await vi.advanceTimersByTimeAsync(999);
      expect(late).toBe(false);
      await vi.advanceTimersByTimeAsync(1);
      await stuck;
      expect(late).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it('terminates the worker and fails open and later requests', async () => {
    const worker = new FakeWorker();
    const bridge = new WasmBridge(worker);
    worker.emit({ type: 'ready' });

    const open = bridge.fetch('http://localhost/api/v1/health');
    await flush();
    const flushing = bridge.flush();
    bridge.terminate();

    expect(worker.terminated).toBe(true);
    await expect(open).rejects.toThrow('stopped');
    await expect(flushing).resolves.toBeUndefined();
    await expect(bridge.fetch('http://localhost/api/v1/health')).rejects.toThrow('stopped');
  });

  it('reports a failed save', () => {
    const worker = new FakeWorker();
    const onSaveFailed = vi.fn();
    new WasmBridge(worker, undefined, { onSaveFailed });
    worker.emit({ type: 'save-failed', message: 'QuotaExceededError' });
    expect(onSaveFailed).toHaveBeenCalledWith('QuotaExceededError');
  });

  it("hands the backend's web push notifications to the page", () => {
    const worker = new FakeWorker();
    const onNotify = vi.fn();
    new WasmBridge(worker, undefined, { onNotify });
    worker.emit({ type: 'notify', payload: '{"title":"A place is free: Tap"}' });
    expect(onNotify).toHaveBeenCalledWith('{"title":"A place is free: Tap"}');
  });

  it('works as the fetch implementation of openapi-fetch', async () => {
    const worker = new FakeWorker();
    worker.respond = (m) =>
      m.url === '/api/v1/health'
        ? {
            type: 'response',
            id: m.id,
            status: 200,
            statusText: '',
            headers: [['Content-Type', 'application/json']],
            body: json({ status: 'ok', version: '1.2.3' }),
          }
        : undefined;
    const bridge = new WasmBridge(worker);
    worker.emit({ type: 'ready' });

    const fetchSpy = vi.fn(bridge.fetch);
    const client = createClient<paths>({ baseUrl: 'http://localhost', fetch: fetchSpy });
    const { data, error } = await client.GET('/api/v1/health');

    expect(error).toBeUndefined();
    expect(data).toEqual({ status: 'ok', version: '1.2.3' });
    expect(fetchSpy.mock.calls[0]?.[0]).toBeInstanceOf(Request);
  });
});
