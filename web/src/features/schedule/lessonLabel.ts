import { useTranslation } from 'react-i18next';
import type { ScheduleLesson } from '@/api/client';
import { formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';

/** What a lesson's label needs of it. */
type Labelled = Pick<ScheduleLesson, 'courseType' | 'startsAt' | 'endsAt'>;

/**
 * Names a lesson in words for dialogs and accessible names: "Ballet,
 * Monday, October 26, 18:00–19:30", in the school time zone.
 */
export function useLessonLabel(): (lesson: Labelled, timeZone: string) => string {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  return (lesson, timeZone) =>
    t('list.cardLabel', {
      courseType: lesson.courseType.name,
      day: formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang),
      time: formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang),
    });
}
