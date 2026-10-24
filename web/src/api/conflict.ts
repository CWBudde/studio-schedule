import { ApiError } from './queries';
import type { components } from './schema';

/**
 * Why the API refused a write with 409: something uses the record, the name
 * is taken, a guardian may not edit a managed dependent, the person has an
 * account already, another lesson uses the room then, a lesson would be in
 * the past, a lesson of a class is to be deleted rather than canceled, the
 * person is booked already, the lesson (or a class, for its members) is
 * full, the lesson is closed for booking (see closedWhy), or the booking is
 * canceled already.
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
