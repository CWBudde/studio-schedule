/**
 * Dedicated module worker that hosts the Go backend compiled to WebAssembly.
 *
 * Startup:
 *  1. fetch `${BASE_URL}wasm_exec.js` and evaluate it in the worker's global
 *     scope (see `loadGoRuntime` for why this is not importScripts/import()),
 *  2. instantiate `${BASE_URL}app.wasm` (streaming, ArrayBuffer fallback),
 *  3. start the Go program (`go.run`, not awaited: it resolves on exit),
 *  4. wait until Go signals readiness via `__studioScheduleReady()` or
 *     `StudioScheduleAPI.ready`, then post `{type: 'ready'}`.
 *
 * Requests that arrive before the backend is ready are queued. If the Go
 * program exits (or panics) a `{type: 'fatal'}` message is posted.
 */
import { createRequestHandler, errorMessage } from './host';
import type { GoBridgeAPI, MainToWorkerMessage, WorkerToMainMessage } from './protocol';

/** Subset of the `Go` class defined by wasm_exec.js that we rely on. */
interface GoRuntime {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
  exit: (code: number) => void;
  exited?: boolean;
}

interface WasmGlobals {
  Go?: new () => GoRuntime;
  StudioScheduleAPI?: GoBridgeAPI;
  __studioScheduleReady?: () => void;
}

const g = globalThis as unknown as WasmGlobals;

const READY_TIMEOUT_MS = 60_000;
const READY_POLL_MS = 25;

let fatalSent = false;
let goExited = false;

function post(msg: WorkerToMainMessage, transfer: Transferable[] = []): void {
  self.postMessage(msg, { transfer });
}

function fatal(message: string): void {
  if (fatalSent) return;
  fatalSent = true;
  post({ type: 'fatal', message });
}

function assetUrl(name: string): string {
  return new URL(`${import.meta.env.BASE_URL}${name}`, self.location.href).href;
}

async function fetchOk(url: string): Promise<Response> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Failed to load ${url} (HTTP ${res.status})`);
  return res;
}

/**
 * wasm_exec.js is a classic (non-module) script that defines `globalThis.Go`.
 * Module workers have no `importScripts()`, and a classic worker cannot use
 * ES imports in the Vite dev server. Importing it with dynamic `import()` is
 * fragile too (Vite refuses JS imports from /public in dev, and the file is
 * not guaranteed to be valid module code). So we fetch the text and evaluate
 * it with an indirect eval, which runs it in the worker's global scope exactly
 * like a classic <script>. This works identically in dev, preview and on
 * GitHub Pages; the only requirement is a CSP that allows 'unsafe-eval' in the
 * worker (Go/WASM already needs 'wasm-unsafe-eval').
 */
async function loadGoRuntime(): Promise<new () => GoRuntime> {
  const source = await (await fetchOk(assetUrl('wasm_exec.js'))).text();
  (0, eval)(source);
  if (typeof g.Go !== 'function') throw new Error('wasm_exec.js did not define globalThis.Go');
  return g.Go;
}

async function instantiate(importObject: WebAssembly.Imports): Promise<WebAssembly.Instance> {
  const url = assetUrl('app.wasm');
  if (typeof WebAssembly.instantiateStreaming === 'function') {
    try {
      const { instance } = await WebAssembly.instantiateStreaming(fetchOk(url), importObject);
      return instance;
    } catch (err) {
      // Typically a wrong Content-Type (not application/wasm); retry without streaming.
      console.warn('[studio-schedule] instantiateStreaming failed, falling back', err);
    }
  }
  const bytes = await (await fetchOk(url)).arrayBuffer();
  const { instance } = await WebAssembly.instantiate(bytes, importObject);
  return instance;
}

/**
 * Resolves once the Go side reports readiness. Must be called before
 * `go.run()` so the callback is installed in time.
 */
function readinessSignal(): { promise: Promise<GoBridgeAPI>; cancel: (err: Error) => void } {
  let resolveFn!: (api: GoBridgeAPI) => void;
  let rejectFn!: (err: Error) => void;
  const promise = new Promise<GoBridgeAPI>((resolve, reject) => {
    resolveFn = resolve;
    rejectFn = reject;
  });

  let done = false;
  const cleanup = () => {
    done = true;
    clearInterval(poll);
    clearTimeout(timeout);
  };
  const succeed = () => {
    if (done) return;
    const api = g.StudioScheduleAPI;
    if (!api || typeof api.fetch !== 'function') {
      fail(new Error('Go reported ready but globalThis.StudioScheduleAPI.fetch is missing'));
      return;
    }
    cleanup();
    resolveFn(api);
  };
  const fail = (err: Error) => {
    if (done) return;
    cleanup();
    rejectFn(err);
  };

  g.__studioScheduleReady = succeed;

  let watchingThenable = false;
  const poll = setInterval(() => {
    const ready = g.StudioScheduleAPI?.ready;
    if (ready === true) {
      succeed();
    } else if (!watchingThenable && ready && typeof ready === 'object' && 'then' in ready) {
      watchingThenable = true;
      ready.then(succeed, (err: unknown) => fail(new Error(errorMessage(err))));
    }
  }, READY_POLL_MS);

  const timeout = setTimeout(() => {
    if (typeof g.StudioScheduleAPI?.fetch === 'function') {
      console.warn('[studio-schedule] no readiness signal from Go, assuming ready');
      succeed();
    } else {
      fail(new Error(`The demo backend did not start within ${READY_TIMEOUT_MS / 1000}s`));
    }
  }, READY_TIMEOUT_MS);

  return { promise, cancel: fail };
}

async function boot(): Promise<GoBridgeAPI> {
  const Go = await loadGoRuntime();
  const go = new Go();

  let exitCode: number | null = null;
  go.exit = (code: number) => {
    exitCode = code;
  };

  const instance = await instantiate(go.importObject);
  const ready = readinessSignal();

  go.run(instance).then(
    () => {
      goExited = true;
      const message = `The demo backend stopped (exit code ${exitCode ?? 'unknown'})`;
      ready.cancel(new Error(message));
      fatal(message);
    },
    (err: unknown) => {
      goExited = true;
      const message = `The demo backend crashed: ${errorMessage(err)}`;
      ready.cancel(new Error(message));
      fatal(message);
    },
  );

  return ready.promise;
}

const handlerPromise = boot().then(
  (api) => {
    post({ type: 'ready' });
    return createRequestHandler(api);
  },
  (err: unknown) => {
    fatal(errorMessage(err));
    throw err;
  },
);
// The rejection is reported via the fatal message; avoid an unhandled rejection.
handlerPromise.catch(() => undefined);

self.addEventListener('message', (event: MessageEvent<MainToWorkerMessage>) => {
  const msg = event.data;
  if (msg?.type !== 'request') return;

  handlerPromise
    .then((handle) => handle(msg))
    .then(
      (reply) => {
        post(reply, reply.type === 'response' ? [reply.body] : []);
        if (reply.type === 'error' && (goExited || /already exited/i.test(reply.message))) {
          fatal(`The demo backend stopped: ${reply.message}`);
        }
      },
      (err: unknown) => post({ type: 'error', id: msg.id, message: errorMessage(err) }),
    );
});
