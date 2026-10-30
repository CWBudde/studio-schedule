/**
 * Dedicated module worker that hosts the Go backend compiled to WebAssembly.
 *
 * Startup:
 *  1. fetch `${BASE_URL}wasm_exec.js` and evaluate it in the worker's global
 *     scope (see `loadGoRuntime` for why this is not importScripts/import()),
 *     while waiting for the page's `{type: 'init'}`, which names the demo
 *     school (the worker cannot read the visitor's choice in localStorage),
 *  2. instantiate `${BASE_URL}app.wasm` (streaming, ArrayBuffer fallback),
 *  3. hand that school's saved data of an earlier visit (IndexedDB, see
 *     persistence.ts) to Go as `__studioScheduleSnapshot`, which Go restores
 *     before it serves anything, and the school as `DEMO_SCHOOL`, which Go
 *     seeds otherwise,
 *  4. start the Go program (`go.run`, not awaited: it resolves on exit),
 *  5. wait until Go signals readiness via `__studioScheduleReady()` or
 *     `StudioScheduleAPI.ready`, then post `{type: 'ready'}`.
 *
 * Requests that arrive before the backend is ready are queued. If the Go
 * program exits (or panics) a `{type: 'fatal'}` message is posted. After
 * every committed change (Go calls `__studioScheduleChanged()`, also for the
 * background jobs) and every new cookie, the store and the cookie jar are
 * saved again; `{type: 'flush'}` saves at once. Only the active tab runs this
 * worker (tabLock.ts); "Reset demo" stops it and deletes the data from the
 * page (persistence.ts).
 */
import type { DemoSchool } from '@/features/demo/school';
import { routerHistory } from '@/lib/env';
import { CookieJar } from './cookieJar';
import { createRequestHandler, errorMessage } from './host';
import {
  claimOrGiveUp,
  clearSnapshot,
  indexedDbStorage,
  SNAPSHOT_FORMAT,
  SnapshotSaver,
  type Claim,
} from './persistence';
import type { GoBridgeAPI, MainToWorkerMessage, WorkerToMainMessage } from './protocol';
import { lazyReceiptPdf } from './receiptPdf';

/** Subset of the `Go` class defined by wasm_exec.js that we rely on. */
interface GoRuntime {
  importObject: WebAssembly.Imports;
  /** Environment of the Go program (os.Getenv). */
  env: Record<string, string>;
  run(instance: WebAssembly.Instance): Promise<void>;
  exit: (code: number) => void;
  exited?: boolean;
}

interface WasmGlobals {
  Go?: new () => GoRuntime;
  StudioScheduleAPI?: GoBridgeAPI;
  __studioScheduleReady?: () => void;
  /** The saved store for Go to restore (internal/wasmbridge.SnapshotGlobal). */
  __studioScheduleSnapshot?: Uint8Array;
  /** Called by Go after each committed change (internal/wasmbridge.ChangedGlobal). */
  __studioScheduleChanged?: () => void;
  /** Called by Go to render a receipt PDF (internal/wasmbridge.RenderPDFGlobal). */
  __studioScheduleRenderPDF?: (input: Uint8Array) => Promise<Uint8Array>;
  /** Called by Go to load the PDF module ahead of a render (internal/wasmbridge.LoadPDFGlobal). */
  __studioScheduleLoadPDF?: () => Promise<void>;
  /** Called by Go to show a web push notification (internal/wasmbridge.NotifyGlobal). */
  __studioScheduleNotify?: (payload: string, endpoint: string) => void;
}

const g = globalThis as unknown as WasmGlobals;

const READY_TIMEOUT_MS = 60_000;
const READY_POLL_MS = 25;

let fatalSent = false;
let goExited = false;

/** The Go program is gone: nothing can be saved any more. */
function goStopped(): void {
  goExited = true;
  saver?.stop();
}

let resolveSchool!: (school: DemoSchool) => void;
/** The demo school, from the page's `init` message. */
const schoolChosen = new Promise<DemoSchool>((resolve) => (resolveSchool = resolve));

const storage = indexedDbStorage();
let saver: SnapshotSaver | null = null;
/** A change committed before the saver exists (the seed, the first jobs). */
let changedEarly = false;

g.__studioScheduleChanged = () => {
  if (saver) saver.schedule();
  else changedEarly = true;
};

function post(msg: WorkerToMainMessage, transfer: Transferable[] = []): void {
  self.postMessage(msg, { transfer });
}

/**
 * The demo's web push (ADR 0017): a worker cannot show notifications, so
 * the page does (bridge.ts).
 */
g.__studioScheduleNotify = (payload: string, endpoint: string) => {
  post({ type: 'notify', payload: String(payload), endpoint: String(endpoint) });
};

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
 * Receipt PDFs: the typst module (about 25 MB) is fetched and compiled when the
 * first receipt is rendered, not at startup.
 */
const receiptPdf = lazyReceiptPdf(async () => {
  const url = assetUrl('receipt.wasm');
  if (typeof WebAssembly.compileStreaming === 'function') {
    try {
      return await WebAssembly.compileStreaming(fetchOk(url));
    } catch (err) {
      console.warn('[studio-schedule] compileStreaming failed, falling back', err);
    }
  }
  return WebAssembly.compile(await (await fetchOk(url)).arrayBuffer());
});
g.__studioScheduleRenderPDF = receiptPdf;
g.__studioScheduleLoadPDF = receiptPdf.ready;

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

interface Booted {
  api: GoBridgeAPI;
  school: DemoSchool;
  claimed: Claim | null;
}

async function boot(): Promise<Booted> {
  // Claim the school's saved data (fencing out any earlier worker); if
  // IndexedDB fails or hangs, start from the seed and tell the visitor that
  // nothing is saved.
  const claiming = schoolChosen.then(async (school) => ({
    school,
    claimed: await claimOrGiveUp(storage, school),
  }));
  const [Go, { school, claimed }] = await Promise.all([loadGoRuntime(), claiming]);
  if (!claimed) post({ type: 'save-failed', message: 'IndexedDB is not available' });
  const go = new Go();
  if (claimed?.snapshot) g.__studioScheduleSnapshot = claimed.snapshot.store;
  go.env = {
    ...go.env,
    // Links in mails (shown in the demo mailbox) point to the app's routes.
    APP_URL: assetUrl(routerHistory === 'hash' ? '#/' : ''),
    // The school to seed; Go refuses a snapshot of another school.
    DEMO_SCHOOL: school,
  };

  let exitCode: number | null = null;
  go.exit = (code: number) => {
    exitCode = code;
  };

  const instance = await instantiate(go.importObject);
  const ready = readinessSignal();

  go.run(instance).then(
    () => {
      goStopped();
      const message = `The demo backend stopped (exit code ${exitCode ?? 'unknown'})`;
      ready.cancel(new Error(message));
      fatal(message);
    },
    (err: unknown) => {
      goStopped();
      const message = `The demo backend crashed: ${errorMessage(err)}`;
      ready.cancel(new Error(message));
      fatal(message);
    },
  );

  return { api: await ready.promise, school, claimed };
}

/**
 * Picks up the session of the saved visit if Go restored its data, and saves
 * both again after every change.
 */
async function persist({ api, school, claimed }: Booted): Promise<CookieJar> {
  const jar = new CookieJar();
  const saved = claimed?.snapshot;
  if (saved && api.restored) {
    jar.load(saved.cookies);
  } else if (saved) {
    // Go refused the data (another build or school): forget it, session included.
    await clearSnapshot(storage, school).catch((err: unknown) =>
      console.warn('[studio-schedule] could not delete old demo data', err),
    );
  }

  const snapshot = api.snapshot?.bind(api);
  if (snapshot && claimed && !goExited) {
    saver = new SnapshotSaver(
      storage,
      school,
      claimed.epoch,
      async () => ({
        format: SNAPSHOT_FORMAT,
        store: await snapshot(),
        cookies: jar.save(),
        savedAt: Date.now(),
      }),
      (err) => post({ type: 'save-failed', message: errorMessage(err) }),
    );
    if (changedEarly) saver.schedule();
  }
  return jar;
}

const handlerPromise = boot().then(
  async (booted) => {
    const jar = await persist(booted);
    post({ type: 'ready' });
    return createRequestHandler(booted.api, jar, () => saver?.schedule());
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
  if (msg?.type === 'init') {
    resolveSchool(msg.school);
    return;
  }
  if (msg?.type === 'flush') {
    const id = msg.id;
    void (saver?.flush() ?? Promise.resolve()).then(() => post({ type: 'flushed', id }));
    return;
  }
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
