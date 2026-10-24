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
 * invite, its invite ran out or was used, or it left the list.
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
