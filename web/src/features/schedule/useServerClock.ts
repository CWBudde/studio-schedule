import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useSyncExternalStore } from 'react';
import { scheduleQueryKey, teachQueryKey } from '@/api/queries';
import { useNow } from '@/lib/useNow';

/**
 * How far the server's clock is ahead of this device's, in milliseconds, as
 * the latest schedule or instructor-view answer in the cache shows it (`now`
 * against the time it arrived); 0 before there is one.
 */
export function useServerClockOffset(): number {
  const cache = useQueryClient().getQueryCache();
  const subscribe = useCallback((onChange: () => void) => cache.subscribe(onChange), [cache]);
  const offset = useCallback(() => {
    let latest: { now: string; at: number } | undefined;
    for (const queryKey of [scheduleQueryKey, teachQueryKey]) {
      for (const query of cache.findAll({ queryKey })) {
        const data = query.state.data as { now: string } | undefined;
        const at = query.state.dataUpdatedAt;
        if (data && (!latest || at > latest.at)) latest = { now: data.now, at };
      }
    }
    return latest ? Date.parse(latest.now) - latest.at : 0;
  }, [cache]);
  return useSyncExternalStore(subscribe, offset);
}

/**
 * The server's current time in milliseconds, ticking like useNow, for views
 * without a schedule or instructor-view answer of their own (such as the lesson sheet): a wrong
 * device clock must not open or close booking.
 */
export function useServerClock(): number {
  return useNow() + useServerClockOffset();
}
