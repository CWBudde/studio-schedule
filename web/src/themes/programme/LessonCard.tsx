import { useId } from 'react';
import {
  AvailabilityBadges,
  hasAvailabilityBadges,
} from '@/features/schedule/booking/AvailabilityBadges';
import {
  lessonCardButton,
  lessonCardSurface,
  useLessonCardLabel,
} from '@/features/schedule/list/cardParts';
import { LessonCardBody } from '@/features/schedule/list/LessonCard';
import { cn } from '@/lib/utils';
import type { LessonCardProps } from '../slots';

/**
 * A lesson of the schedule as one button that opens its sheet; ended and
 * canceled lessons are muted. There is no quick action: it goes unused.
 */
export function LessonCard({ lesson, past, now, timeZone, onOpen }: LessonCardProps) {
  const detailsId = useId();
  const availabilityId = useId();
  const label = useLessonCardLabel()(lesson, timeZone);
  const canceled = lesson.status === 'canceled';
  // Canceled and ended lessons lose their accent colors.
  const quiet = canceled || past;

  return (
    <button
      type="button"
      aria-label={label}
      aria-describedby={
        hasAvailabilityBadges(lesson, !quiet, now) ? `${detailsId} ${availabilityId}` : detailsId
      }
      onClick={onOpen}
      data-canceled={canceled || undefined}
      data-past={past || undefined}
      className={cn(
        lessonCardSurface,
        lessonCardButton,
        'w-full transition-[background-color,box-shadow] hover:ring-primary/30',
      )}
    >
      <LessonCardBody lesson={lesson} timeZone={timeZone} detailsId={detailsId} quiet={quiet}>
        <AvailabilityBadges id={availabilityId} lesson={lesson} showFree={!quiet} now={now} />
      </LessonCardBody>
    </button>
  );
}
