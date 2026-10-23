import { useSyncExternalStore } from 'react';

/** Below Tailwind's `sm` breakpoint. */
const phoneQuery = '(max-width: 639.98px)';

function subscribe(onChange: () => void) {
  const media = window.matchMedia(phoneQuery);
  media.addEventListener('change', onChange);
  return () => media.removeEventListener('change', onChange);
}

/** Whether the viewport is phone-sized; follows rotations and resizes. */
export function useIsPhone(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(phoneQuery).matches,
    () => false,
  );
}
