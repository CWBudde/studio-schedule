import { useEffect } from 'react';
import { refreshPushSubscription } from '@/api/queries';
import { useMe } from '@/api/useMe';
import { pushOwner } from './device';

const LAST_KEY = 'studio-schedule-push-refreshed';
const DAY = 24 * 60 * 60 * 1000;

/**
 * Once a day, while the account that turned notifications on here is
 * signed in, saves this device's subscription again: the server drops
 * subscriptions unused for six months (ADR 0017).
 */
export function usePushKeepAlive(): void {
  const accountId = useMe().data?.accountId;

  useEffect(() => {
    if (!accountId || pushOwner() !== accountId) return;
    try {
      const last = Number(localStorage.getItem(LAST_KEY) ?? 0);
      if (Date.now() - last < DAY) return;
      localStorage.setItem(LAST_KEY, String(Date.now()));
    } catch {
      return;
    }
    void refreshPushSubscription(accountId);
  }, [accountId]);
}
