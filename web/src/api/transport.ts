import { onlineManager } from '@tanstack/react-query';
import { transport } from '@/lib/env';
import { isWrite, OfflineError } from '@/lib/online';
import { setBackendStatus } from './backendStatus';
import { WasmBridge } from './wasm/bridge';
import { showDemoNotification } from './wasm/demoNotification';
import { clearSnapshot, indexedDbStorage, resetDemoData } from './wasm/persistence';
import { TAKEOVER_WAIT_MS, TabLock } from './wasm/tabLock';

/** Something the visitor of the web demo should hear about once. */
export type DemoNotice = 'save-failed' | 'reset-failed';

/**
 * Whether this tab runs the web demo: 'pending' until the tab lock is
 * decided, 'active' while this tab holds it, 'elsewhere' when another tab does.
 */
export type DemoTab = 'pending' | 'active' | 'elsewhere';

/** What the web demo offers beyond the API; `demoControls` is null over HTTP. */
export interface DemoControls {
  /**
   * Deletes the demo's saved data (store and session) and reloads the page,
   * which starts the demo again from its seed.
   */
  reset(): Promise<void>;
  /** Makes this tab the one that runs the demo (it reloads). */
  takeOver(): void;
  /** Delivers notices, including those raised before anyone listened. */
  subscribeNotices(listener: (notice: DemoNotice) => void): () => void;
  /** Whether this tab runs the demo (for `useSyncExternalStore`). */
  getTab(): DemoTab;
  subscribeTab(listener: () => void): () => void;
}

// Survive the reload of takeOver and reset.
const TAKEOVER_KEY = 'studio-schedule-demo-takeover';
const RESET_FAILED_KEY = 'studio-schedule-demo-reset-failed';

let bridge: WasmBridge | null = null;
let active: Promise<boolean> | null = null;

let tab: DemoTab = 'pending';
const tabListeners = new Set<() => void>();

function setTab(next: DemoTab): void {
  tab = next;
  for (const listener of tabListeners) listener();
}

const pendingNotices: DemoNotice[] = [];
const noticeListeners = new Set<(notice: DemoNotice) => void>();

function notify(notice: DemoNotice): void {
  if (noticeListeners.size === 0) pendingNotices.push(notice);
  for (const listener of noticeListeners) listener(notice);
}

function takeFlag(key: string): boolean {
  try {
    const set = sessionStorage.getItem(key) !== null;
    sessionStorage.removeItem(key);
    return set;
  } catch {
    return false;
  }
}

function setFlag(key: string): void {
  try {
    sessionStorage.setItem(key, '1');
  } catch {
    // Without session storage the flag is lost; the reload still happens.
  }
}

function startBridge(): WasmBridge {
  // Vite bundles the worker (and its imports) because of this exact pattern.
  const worker = new Worker(new URL('./wasm/worker.ts', import.meta.url), {
    type: 'module',
    name: 'studio-schedule-backend',
  });
  const started = new WasmBridge(worker, setBackendStatus, {
    onSaveFailed: () => notify('save-failed'),
    onNotify: (payload, endpoint) => void showDemoNotification(payload, endpoint),
  });
  // Best effort: save what is pending before the tab goes away.
  const flush = () => void started.flush();
  window.addEventListener('pagehide', flush);
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') flush();
  });
  return started;
}

function stopBridge(): void {
  bridge?.terminate();
  bridge = null;
}

/**
 * Makes this tab the one that runs the demo, if no other tab does (or if the
 * visitor chose "Use here"), and starts the backend. Only that tab saves.
 */
function activate(): Promise<boolean> {
  active ??= (async () => {
    if (takeFlag(RESET_FAILED_KEY)) notify('reset-failed');
    const lock = new TabLock({
      locks: globalThis.navigator?.locks,
      channel:
        typeof BroadcastChannel === 'function'
          ? new BroadcastChannel('studio-schedule-demo')
          : undefined,
      onHandOver: async () => {
        // Let an in-flight save finish, but stop before the other tab gives
        // up waiting and takes the lock by force.
        await bridge?.flush(TAKEOVER_WAIT_MS - 500);
        stopBridge();
      },
      onLost: () => {
        stopBridge();
        setTab('elsewhere');
        setBackendStatus({ state: 'elsewhere' });
      },
    });
    const ok = await lock.acquire(takeFlag(TAKEOVER_KEY));
    if (ok) {
      setTab('active');
      bridge = startBridge();
    } else {
      setTab('elsewhere');
      setBackendStatus({ state: 'elsewhere' });
    }
    return ok;
  })();
  return active;
}

/** Starts the in-browser backend in WASM mode; a no-op for HTTP. */
export function startBackend(): void {
  if (transport === 'wasm') void activate();
}

export const demoControls: DemoControls | null =
  transport === 'wasm'
    ? {
        reset: () =>
          resetDemoData({
            // This tab holds the lock, so no other tab saves meanwhile.
            stop: stopBridge,
            clear: () => clearSnapshot(indexedDbStorage()),
            markFailed: () => setFlag(RESET_FAILED_KEY),
            reload: () => window.location.reload(),
          }),
        takeOver: () => {
          setFlag(TAKEOVER_KEY);
          window.location.reload();
        },
        subscribeNotices: (listener) => {
          noticeListeners.add(listener);
          for (const notice of pendingNotices.splice(0)) listener(notice);
          return () => noticeListeners.delete(listener);
        },
        getTab: () => tab,
        subscribeTab: (listener) => {
          tabListeners.add(listener);
          return () => tabListeners.delete(listener);
        },
      }
    : null;

async function wasmFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  await activate();
  if (!bridge) throw new TypeError('The demo runs in another tab');
  return bridge.fetch(input, init);
}

/**
 * `fetch` used by the API client: the browser's fetch in HTTP mode, the
 * worker-hosted Go backend in WASM mode. Offline it sends no write at all
 * (OfflineError): the app is read-only then (ADR 0017), also in the demo,
 * whose backend runs in the browser.
 */
export function transportFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const method = init?.method ?? (input instanceof Request ? input.method : 'GET');
  if (isWrite(method) && !onlineManager.isOnline()) return Promise.reject(new OfflineError());
  return transport === 'wasm' ? wasmFetch(input, init) : globalThis.fetch(input, init);
}
