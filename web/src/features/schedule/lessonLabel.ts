import { useTranslation } from 'react-i18next';
import type { ScheduleLesson } from '@/api/client';
import { formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';

/** What a lesson's title needs of it: any lesson's course type, if it has one. */
export type Titled = { courseType?: { name: string } };

/** What a lesson's label needs of it. */
type Labelled = Titled & Pick<ScheduleLesson, 'startsAt' | 'endsAt'>;

/**
 * Titles a lesson: its course type's name, or "Room rental" for a lesson
 * without one (only a room rental may have none).
 */
export function useLessonTitle(): (lesson: Titled) => string {
  const { t } = useTranslation('schedule');
  return (lesson) => lesson.courseType?.name ?? t('kind.rental');
}

/**
 * Whether a lesson shows its kind beside its title: any kind but a regular
 * lesson, unless the title already is the kind (a room rental without a
 * course type).
 */
export const showsKind = (lesson: Titled & Pick<ScheduleLesson, 'kind'>) =>
  lesson.kind !== 'regular' && lesson.courseType !== undefined;

/**
 * Names a lesson in words for dialogs and accessible names: "Ballet,
 * Monday, October 26, 18:00–19:30", in the school time zone.
 */
export function useLessonLabel(): (lesson: Labelled, timeZone: string) => string {
  const { t, i18n } = useTranslation('schedule');
  const title = useLessonTitle();
  const lang = i18n.resolvedLanguage ?? 'en';
  return (lesson, timeZone) =>
    t('list.cardLabel', {
      courseType: title(lesson),
      day: formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang),
      time: formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang),
    });
}
