import { useSyncExternalStore } from 'react';

// Whether the demo mailbox sheet is open. Shared so that pages can open the
// mailbox from the header (e.g. after registering, to find the link).
let open = false;
const listeners = new Set<() => void>();

export function setDemoMailboxOpen(value: boolean): void {
  if (open === value) return;
  open = value;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

const getOpen = () => open;

/** Whether the demo mailbox sheet is open. */
export function useDemoMailboxOpen(): boolean {
  return useSyncExternalStore(subscribe, getOpen, getOpen);
}
