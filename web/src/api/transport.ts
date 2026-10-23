import { transport } from '@/lib/env';
import { setBackendStatus } from './backendStatus';
import { WasmBridge } from './wasm/bridge';

let bridge: WasmBridge | null = null;

function getBridge(): WasmBridge {
  if (!bridge) {
    // Vite bundles the worker (and its imports) because of this exact pattern.
    const worker = new Worker(new URL('./wasm/worker.ts', import.meta.url), {
      type: 'module',
      name: 'studio-schedule-backend',
    });
    bridge = new WasmBridge(worker, setBackendStatus);
  }
  return bridge;
}

/** Starts the in-browser backend in WASM mode; a no-op for HTTP. */
export function startBackend(): void {
  if (transport === 'wasm') getBridge();
}

/**
 * `fetch` used by the API client: the browser's fetch in HTTP mode, the
 * worker-hosted Go backend in WASM mode.
 */
export function transportFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return transport === 'wasm' ? getBridge().fetch(input, init) : globalThis.fetch(input, init);
}
