import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { ScheduleLesson } from '@/api/client';
import { meQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

/** Whether AvailabilityBadges shows anything, e.g. to refer to it only then. */
export function hasAvailabilityBadges(lesson: ScheduleLesson, showFree: boolean): boolean {
  return showFree || (lesson.bookedFor ?? []).length > 0;
}

/**
 * The free places of a lesson ("3 free", "Full") and whom of the signed-in
 * family it is booked for ("Booked", "Booked: Mia Sample"). The free places
 * are left out for canceled and ended lessons, where they mean nothing.
 * `id` names the badges, e.g. for the card's aria-describedby (see
 * hasAvailabilityBadges). Until it is known who is signed in, a booking
 * shows as plain "Booked".
 */
export function AvailabilityBadges({
  id,
  lesson,
  showFree,
  className,
}: {
  id?: string;
  lesson: ScheduleLesson;
  showFree: boolean;
  className?: string;
}) {
  const { t } = useTranslation('schedule');
  const me = useQuery(meQuery);
  const booked = lesson.bookedFor ?? [];
  if (!hasAvailabilityBadges(lesson, showFree)) return null;

  const self = me.data?.personId;
  const others = booked.filter((b) => b.personId !== self).map((b) => b.name);
  const selfBooked = booked.length > others.length;
  const bookedText =
    self === undefined || others.length === 0
      ? t('booking.badge.booked')
      : t('booking.badge.bookedFor', {
          names: [...(selfBooked ? [t('booking.badge.you')] : []), ...others].join(', '),
        });

  return (
    <span id={id} className={cn('flex max-w-full flex-wrap gap-1', className)}>
      {booked.length > 0 && (
        <Badge className="max-w-full">
          <span className="truncate">{bookedText}</span>
        </Badge>
      )}
      {/* Separates the badges in the description; a flex container does not render it. */}{' '}
      {showFree &&
        (lesson.freePlaces > 0 ? (
          <Badge variant="outline">{t('booking.badge.free', { count: lesson.freePlaces })}</Badge>
        ) : (
          <Badge variant="secondary">{t('booking.badge.full')}</Badge>
        ))}
    </span>
  );
}
