import { useTranslation } from 'react-i18next';
import { closedWhy, conflictReason } from '@/api/conflict';
import { ApiError, refusedAsStarted, refusedAt } from '@/api/queries';

/**
 * Why a booking write failed, in words: every 409 reason (and the `why` of a
 * `closed`), a 404, a 422 for a lesson that has started or an end date
 * before the lesson, or a general sentence. `name` is the person the write was for.
 */
export function useBookingErrorText(): (error: unknown, name: string) => string {
  const { t } = useTranslation('schedule');
  return (error, name) => {
    const why = closedWhy(error);
    if (why) return t(`booking.errors.closed.${why}`);
    const reason = conflictReason(error);
    if (reason === 'already-booked') return t('booking.errors.alreadyBooked', { name });
    if (reason === 'full') return t('booking.errors.full');
    if (reason === 'closed') return t('booking.errors.closed.cutoff');
    if (reason === 'already-canceled') return t('booking.errors.alreadyCanceled');
    if (error instanceof ApiError && error.status === 404) return t('booking.errors.notFound');
    if (refusedAsStarted(error, 'path.bookingId')) return t('booking.errors.started');
    if (refusedAt(error, 'query.until') || refusedAt(error, 'body.until')) {
      return t('booking.errors.untilBeforeLesson');
    }
    return t('booking.errors.failed');
  };
}
