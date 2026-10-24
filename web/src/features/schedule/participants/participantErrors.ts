import { useTranslation } from 'react-i18next';
import { closedWhy, conflictReason, invalidReason } from '@/api/conflict';
import { ApiError, refusedAt } from '@/api/queries';

/**
 * Why a staff write to a lesson's participants failed, in words. `name` is
 * the participant the write was for.
 */
export function useParticipantErrorText(): (error: unknown, { name }: { name: string }) => string {
  const { t } = useTranslation('schedule');
  return (error, { name }) => {
    if (closedWhy(error) === 'canceled') return t('participants.errors.canceledLesson');
    const reason = conflictReason(error);
    if (reason === 'already-booked') return t('participants.errors.alreadyBooked', { name });
    if (reason === 'already-canceled') return t('participants.errors.alreadyCanceled');
    if (reason === 'full') return t('participants.errors.full');
    if (error instanceof ApiError && error.status === 404) return t('participants.errors.notFound');
    const refusal = invalidReason(error);
    if (refusal === 'day-over') return t('participants.errors.dayOver');
    if (refusal === 'not-started') return t('participants.errors.notStarted');
    // A refusal at the path without a reason: neither guess is safe.
    if (refusedAt(error, 'path.lessonId') || refusedAt(error, 'path.bookingId')) {
      return t('participants.errors.notNow');
    }
    if (refusedAt(error, 'body.personId')) return t('participants.errors.unknownPerson');
    return t('participants.errors.failed');
  };
}
