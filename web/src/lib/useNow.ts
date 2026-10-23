import { useEffect, useState } from 'react';

/**
 * The current time in milliseconds, refreshed every `interval` ms and when
 * the page becomes visible again (timers of hidden tabs are throttled), for
 * views that mark what is past (default: once a minute).
 */
export function useNow(interval = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const refresh = () => setNow(Date.now());
    const onVisible = () => {
      if (document.visibilityState === 'visible') refresh();
    };
    const timer = setInterval(refresh, interval);
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [interval]);
  return now;
}

/**
 * The server's current time in milliseconds, ticking like useNow: the client
 * clock corrected by the difference between the server's `serverNow` and the
 * client time the response arrived (`receivedAt`, e.g. a query's
 * dataUpdatedAt), so a wrong browser clock does not shift it.
 */
export function useServerNow(serverNow: string, receivedAt: number): number {
  return useNow() + Date.parse(serverNow) - receivedAt;
}
