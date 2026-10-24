import { FormError } from '@/features/auth/FormError';
import { type ConflictReason, conflictReason } from '@/api/conflict';

/**
 * Why a write failed, in words: the sentence of `conflicts` for the reason
 * of a 409, the message of `messages` for other statuses, or a general one.
 * Never the API's detail text.
 */
export function ProblemAlert({
  error,
  conflicts = {},
  messages = {},
}: {
  error: unknown;
  conflicts?: Partial<Record<ConflictReason, string>>;
  messages?: Partial<Record<number, string>>;
}) {
  const reason = conflictReason(error);
  const conflict = reason && conflicts[reason];
  return (
    <FormError error={error} messages={conflict ? { ...messages, 409: conflict } : messages} />
  );
}
