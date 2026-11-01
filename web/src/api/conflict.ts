import { ApiError } from './queries';
import type { components } from './schema';

/**
 * Why the API refused a write with 409: something uses the record, the name
 * is taken, a guardian may not edit a managed dependent, the person has an
 * account already, another lesson uses the room then, a lesson would be in
 * the past, a lesson of a class is to be deleted rather than canceled, the
 * person is booked already, the lesson (or a class, for its members) is
 * full, the lesson is closed for booking (see closedWhy), the booking is
 * canceled already, or a waiting-list write does not fit the entry: the
 * lesson has a free place, the person waits already, the entry has no
 * invite, its invite ran out or was used, or it left the list; or the
 * pass is voided already; or a receipt cannot be issued (the receipt
 * settings are incomplete) or canceled (its pass paid for visits, it is
 * canceled already, or it is a cancellation itself); or the time zone or
 * currency is fixed by lessons or prices and receipts (ADR 0013); or a merge
 * of two persons is refused (both have an account, or a guardian would
 * become a dependent); or a person is anonymized already or is the only
 * guardian of a dependent, or an account change would leave the school
 * without an enabled admin or concerns the admin's own account (ADR 0016);
 * or an event write is refused (ADR 0019): the lesson is an event's
 * session, the event has no session to publish, is not a draft or not
 * published, the registration is paid already or on the waiting list, its
 * receipt is canceled while it is open, or the event has started.
 */
export type ConflictReason = components['schemas']['Conflict']['reason'];

const conflictReasons: readonly ConflictReason[] = [
  'in-use',
  'duplicate',
  'managed',
  'has-account',
  'room-taken',
  'past',
  'generated',
  'already-booked',
  'full',
  'closed',
  'already-canceled',
  'not-full',
  'already-waiting',
  'not-invited',
  'invite-expired',
  'invite-used',
  'not-waiting',
  'already-voided',
  'receipt-settings-incomplete',
  'pass-used',
  'cancellation',
  'timezone-locked',
  'currency-locked',
  'mail-limit',
  'expired',
  'not-failed',
  'secret-link',
  'two-accounts',
  'guardian-cycle',
  'anonymized',
  'sole-guardian',
  'last-admin',
  'own-account',
  'event-session',
  'no-sessions',
  'not-draft',
  'not-published',
  'already-paid',
  'event-attended',
  'registration-open',
  'not-registered',
];

/** The lesson that uses the room, named by a 409 `room-taken`. */
export type ConflictLesson = components['schemas']['ConflictLesson'];

/** The reason of a 409 answer of a write, if it was one. */
export function conflictReason(error: unknown): ConflictReason | undefined {
  if (!(error instanceof ApiError) || error.status !== 409) return undefined;
  const reason = (error.body as { reason?: unknown } | undefined)?.reason;
  return conflictReasons.find((r) => r === reason);
}

/** The lesson a 409 `room-taken` names, if the error is one. */
export function roomTakenBy(error: unknown): ConflictLesson | undefined {
  if (conflictReason(error) !== 'room-taken') return undefined;
  return (error as ApiError & { body: { lesson?: ConflictLesson } }).body.lesson;
}

/** Why a lesson cannot be booked, named by a 409 `closed`. */
export type ClosedWhy = NonNullable<components['schemas']['Conflict']['why']>;

const closedWhys: readonly ClosedWhy[] = ['cutoff', 'release', 'canceled', 'started'];

/** Why the lesson is closed for booking, if the error is a 409 `closed` that says so. */
export function closedWhy(error: unknown): ClosedWhy | undefined {
  if (conflictReason(error) !== 'closed') return undefined;
  const why = (error as ApiError & { body: { why?: unknown } }).body.why;
  return closedWhys.find((w) => w === why);
}

/**
 * Why the API refused a write with 422 although the request was fine: the
 * lesson has not started yet, or an instructor's school day of it is over.
 */
export type InvalidReason = NonNullable<components['schemas']['Invalid']['reason']>;

const invalidReasons: readonly InvalidReason[] = ['not-started', 'day-over'];

/** The reason of a 422 answer of a write, if it gives one (a 422 about the body does not). */
export function invalidReason(error: unknown): InvalidReason | undefined {
  if (!(error instanceof ApiError) || error.status !== 422) return undefined;
  const reason = (error.body as { reason?: unknown } | undefined)?.reason;
  return invalidReasons.find((r) => r === reason);
}
