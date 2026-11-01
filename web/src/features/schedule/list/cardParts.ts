import { useTranslation } from 'react-i18next';
import type { components } from '@/api/schema';
import type { AudienceFields } from '@/lib/audience';
import { showsKind, useLessonLabel } from '../lessonLabel';

/** What a lesson card shows of a lesson (the schedule's lessons and "My lessons" have it). */
export type CardLesson = AudienceFields &
  Pick<
    components['schemas']['LessonSummary'],
    | 'courseType'
    | 'kind'
    | 'status'
    | 'visibility'
    | 'startsAt'
    | 'endsAt'
    | 'room'
    | 'location'
    | 'instructor'
    | 'accompanist'
    | 'renter'
  >;

/**
 * The card's surface: the thin primary line on the left is a barre; ended
 * (`data-past`) and canceled (`data-canceled`) lessons lose it and fade.
 */
export const lessonCardSurface =
  'rounded-lg bg-card text-sm text-card-foreground shadow-[inset_2px_0_0_0_var(--color-primary)] ring-1 ring-foreground/10 data-canceled:bg-muted/40 data-canceled:text-muted-foreground data-canceled:shadow-none data-past:bg-muted/40 data-past:text-muted-foreground data-past:shadow-none';

/** The button that opens the lesson, inside a card or as the card itself. */
export const lessonCardButton =
  'flex flex-col gap-2 rounded-lg p-4 pl-5 text-left transition-colors outline-none hover:bg-accent/50 focus-visible:ring-3 focus-visible:ring-ring/50';

/**
 * The accessible name of a lesson card: "Ballet, Monday, October 26,
 * 18:00–19:30", then whether it is canceled (`canceledLabel`, by default
 * "Canceled"), its kind unless a regular class or already its title, a room
 * rental's renter, and
 * its visibility unless public.
 */
export function useLessonCardLabel(): (
  lesson: CardLesson,
  timeZone: string,
  canceledLabel?: string,
) => string {
  const { t } = useTranslation('schedule');
  const lessonLabel = useLessonLabel();
  return (lesson, timeZone, canceledLabel) =>
    [
      lessonLabel(lesson, timeZone),
      lesson.status === 'canceled' && (canceledLabel ?? t('status.canceled')),
      showsKind(lesson) && t(`kind.${lesson.kind}`),
      lesson.renter,
      lesson.visibility !== 'public' && t(`visibility.${lesson.visibility}`),
    ]
      .filter(Boolean)
      .join(', ');
}
