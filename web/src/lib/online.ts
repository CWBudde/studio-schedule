import { onlineManager } from '@tanstack/react-query';
import { useSyncExternalStore } from 'react';

/**
 * Whether the browser is online, as TanStack Query sees it: offline, the
 * queries pause (they show what the persisted cache restored) and writes
 * are refused (ADR 0017). The same source for both keeps the UI and the
 * requests in step.
 */
export function useOnline(): boolean {
  return useSyncExternalStore(
    (listener) => onlineManager.subscribe(listener),
    () => onlineManager.isOnline(),
    () => true,
  );
}

/**
 * Makes TanStack Query follow `navigator.onLine`, from the start: by
 * itself it assumes online until an `offline` event, so a page loaded
 * offline would ask the network.
 */
export function watchOnline(): void {
  if (typeof window === 'undefined') return;
  onlineManager.setEventListener((setOnline) => {
    const update = () => setOnline(navigator.onLine);
    update();
    window.addEventListener('online', update);
    window.addEventListener('offline', update);
    return () => {
      window.removeEventListener('online', update);
      window.removeEventListener('offline', update);
    };
  });
}

/** Thrown for a write while the browser is offline: nothing was sent. */
export class OfflineError extends TypeError {
  constructor() {
    super('Offline: the change was not sent');
    this.name = 'OfflineError';
  }
}

/** Whether a request of this method changes data. */
export function isWrite(method: string): boolean {
  const m = method.toUpperCase();
  return m !== 'GET' && m !== 'HEAD' && m !== 'OPTIONS';
}
