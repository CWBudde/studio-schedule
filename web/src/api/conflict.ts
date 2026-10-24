import { ApiError } from './queries';
import type { components } from './schema';

/**
 * Why the API refused a write with 409: something uses the record, the name
 * is taken, a guardian may not edit a managed dependent, or the person has
 * an account already.
 */
export type ConflictReason = components['schemas']['Conflict']['reason'];

const conflictReasons: readonly ConflictReason[] = [
  'in-use',
  'duplicate',
  'managed',
  'has-account',
];

/** The reason of a 409 answer of a write, if it was one. */
export function conflictReason(error: unknown): ConflictReason | undefined {
  if (!(error instanceof ApiError) || error.status !== 409) return undefined;
  const reason = (error.body as { reason?: unknown } | undefined)?.reason;
  return conflictReasons.find((r) => r === reason);
}
