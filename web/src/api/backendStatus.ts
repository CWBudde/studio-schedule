import { useSyncExternalStore } from 'react';
import { transport } from '@/lib/env';

export type BackendStatus =
  | { state: 'loading' }
  | { state: 'ready' }
  | { state: 'error'; message: string }
  // The web demo runs in another tab (only one tab runs it at a time).
  | { state: 'elsewhere' };

type Listener = () => void;

// In HTTP mode the backend is a separate server: nothing to boot, ready at once.
let current: BackendStatus = transport === 'wasm' ? { state: 'loading' } : { state: 'ready' };
const listeners = new Set<Listener>();

export function getBackendStatus(): BackendStatus {
  return current;
}

export function setBackendStatus(status: BackendStatus): void {
  current = status;
  for (const listener of listeners) listener();
}

function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Startup state of the backend ('loading' only happens in the WASM demo). */
export function useBackendStatus(): BackendStatus {
  return useSyncExternalStore(subscribe, getBackendStatus, getBackendStatus);
}
