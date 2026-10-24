import { useTranslation } from 'react-i18next';
import type { ClassData, CourseType } from '@/api/queries';
import { weekdayName } from '@/features/timetable/timetable';

/**
 * Names a class for the office's buttons, dialogs and toasts by its course
 * type, weekday and start time, e.g. "Ballet, Monday 18:00".
 */
export function useClassLabel(courseTypes: CourseType[]) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  return (c: Pick<ClassData, 'courseTypeId' | 'weekday' | 'startTime'>) =>
    t('classes.label', {
      courseType: courseTypes.find((x) => x.id === c.courseTypeId)?.name ?? t('classes.unknown'),
      day: weekdayName(c.weekday, lang),
      time: c.startTime,
    });
}
