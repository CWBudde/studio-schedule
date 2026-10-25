import type { Pass } from '@/api/queries';
import type { SchoolDate } from '@/lib/schoolTime';

/** What the pass functions need of a pass; the office's and the family's passes both have it. */
export type PassFacts = Pick<
  Pass,
  'id' | 'status' | 'usable' | 'kind' | 'visitsRemaining' | 'validFrom' | 'validUntil' | 'soldAt'
> & { owner: { id: string } };

/**
 * The state of a pass on the school date `today`, as the UI names it:
 * usable, voided by the office, not valid yet, used up (a visit pass
 * without a visit left) or expired. `usable` is the API's word for today.
 */
export type PassState = 'usable' | 'voided' | 'notYetValid' | 'usedUp' | 'expired';

export function passState(pass: PassFacts, today: SchoolDate): PassState {
  if (pass.status === 'voided') return 'voided';
  if (pass.usable) return 'usable';
  if (pass.validFrom > today) return 'notYetValid';
  if (pass.kind === 'visits' && pass.visitsRemaining === 0) return 'usedUp';
  if (pass.validUntil !== undefined && pass.validUntil < today) return 'expired';
  // The API's today and ours differ (e.g. around midnight): trust the API.
  return 'expired';
}

/**
 * Whether the pass pays for a lesson on the school date `date`: it is
 * active, the date lies within its days, and a visit pass has a visit left
 * (as `domain.Pass.Covers`).
 */
export function covers(pass: PassFacts, date: SchoolDate): boolean {
  return (
    pass.status === 'active' &&
    pass.validFrom <= date &&
    (pass.validUntil === undefined || date <= pass.validUntil) &&
    (pass.kind === 'time' || pass.visitsRemaining > 0)
  );
}

/**
 * The pass of `personId` that would pay for a lesson on `date`, as the API
 * picks it: of the passes that cover the date, time passes first (they
 * spend no visit), then the one that expires first (none last), then the
 * oldest sale, then the ID. Undefined if none covers it.
 */
export function pickPass<P extends PassFacts>(
  passes: readonly P[],
  personId: string,
  date: SchoolDate,
): P | undefined {
  const candidates = passes.filter((p) => p.owner.id === personId && covers(p, date));
  const rank = (p: P) => (p.kind === 'time' ? 0 : 1);
  return candidates.sort(
    (a, b) =>
      rank(a) - rank(b) ||
      compareLastDay(a.validUntil, b.validUntil) ||
      Date.parse(a.soldAt) - Date.parse(b.soldAt) ||
      (a.id < b.id ? -1 : a.id > b.id ? 1 : 0),
  )[0];
}

/** Earlier last days first; a pass without expiry after every other. */
function compareLastDay(a: SchoolDate | undefined, b: SchoolDate | undefined): number {
  if (a === b) return 0;
  if (a === undefined) return 1;
  if (b === undefined) return -1;
  return a.localeCompare(b);
}
