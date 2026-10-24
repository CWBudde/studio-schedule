import type { WaitlistFor } from '@/api/queries';

/**
 * Whether the invite of a waiting-list entry has run out by `now` (the
 * server's clock, in ms): the API says so, or its expiry has passed before
 * the API noticed.
 */
export function inviteRanOut(entry: WaitlistFor, now: number): boolean {
  return (
    entry.status === 'expired' ||
    (entry.status === 'invited' &&
      entry.expiresAt !== undefined &&
      now >= Date.parse(entry.expiresAt))
  );
}

/** Whether the entry holds a place for its person now: invited, and not run out. */
export function isInvited(entry: WaitlistFor, now: number): boolean {
  return entry.status === 'invited' && !inviteRanOut(entry, now);
}

/**
 * Whether the entry keeps its person on the list: anything but an invite
 * that ran out without staying on the list, which is about to end (its
 * person may book or join again).
 */
export function staysOnList(entry: WaitlistFor, now: number): boolean {
  return !inviteRanOut(entry, now) || entry.stayOnList;
}
