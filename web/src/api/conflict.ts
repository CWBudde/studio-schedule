import { ApiError } from './queries';
import type { components } from './schema';

/**
 * Why the API refused a write with 409: something uses the record, the name
 * is taken, a guardian may not edit a managed dependent, the person has an
 * account already, another lesson uses the room then, a lesson would be in
 * the past, or a lesson of a class is to be deleted rather than canceled.
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
