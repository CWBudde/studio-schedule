import { useTranslation } from 'react-i18next';
import { type ConflictReason, conflictReason } from '@/api/conflict';
import { statusOf } from '@/features/auth/form';

/** The sentences for a failed write: by 409 reason, and by HTTP status. */
export type ProblemTexts = {
  conflicts?: Partial<Record<ConflictReason, string>>;
  messages?: Partial<Record<number, string>>;
};

/** `messages`, with the sentence of `conflicts` for a 409's reason as its 409. */
export function problemMessages(
  error: unknown,
  { conflicts = {}, messages = {} }: ProblemTexts,
): Partial<Record<number, string>> {
  const reason = conflictReason(error);
  const conflict = reason && conflicts[reason];
  return conflict ? { ...messages, 409: conflict } : messages;
}

/**
 * Why a write failed, as the title of ProblemAlert (and FormError) says it:
 * the sentence for its 409 reason or status, "too many attempts" for a 429,
 * or a general one; never the API's detail text. For a toast, which has no
 * alert to show.
 */
export function useProblemText() {
  const { t } = useTranslation('auth');
  return (error: unknown, texts: ProblemTexts = {}): string => {
    const messages = problemMessages(error, texts);
    const status = statusOf(error);
    if (status === 429) return messages[429] ?? t('form.tooMany.title');
    return (status !== undefined && messages[status]) || t('form.failed');
  };
}
