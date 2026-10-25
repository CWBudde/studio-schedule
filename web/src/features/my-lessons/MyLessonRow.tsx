import { type ReactNode, useId } from 'react';
import { useTranslation } from 'react-i18next';
import type { components } from '@/api/schema';
import { Badge } from '@/components/ui/badge';
import {
  lessonCardButton,
  lessonCardSurface,
  useLessonCardLabel,
} from '@/features/schedule/list/cardParts';
import { LessonCardBody } from '@/features/schedule/list/LessonCard';
import type { SchoolDate } from '@/lib/schoolTime';
import { cn } from '@/lib/utils';

/** A status badge of a row. */
export type RowBadge = {
  label: string;
  variant: 'default' | 'secondary' | 'destructive' | 'outline';
};

/**
 * A lesson of "My lessons": the schedule's lesson card (a lesson the school
 * canceled says so) as a button that opens the lesson, with the `person` it
 * is for (left out for a family of one) and status `badges`, and beside or
 * below it `children`, the actions. `withDate` shows the date too, for lists
 * not grouped by day (its year only outside the year of `today`); `past`
 * mutes a lesson that is over.
 */
export function MyLessonRow({
  lesson,
  timeZone,
  today,
  withDate = false,
  person,
  badges,
  past = false,
  onOpen,
  children,
}: {
  lesson: components['schemas']['LessonSummary'];
  timeZone: string;
  today: SchoolDate;
  withDate?: boolean;
  person?: string;
  badges: RowBadge[];
  past?: boolean;
  onOpen: () => void;
  children?: ReactNode;
}) {
  const { t } = useTranslation('myLessons');
  const detailsId = useId();
  const canceled = lesson.status === 'canceled';
  const canceledLabel = t('status.schoolCanceled');
  const lessonLabel = useLessonCardLabel()(lesson, timeZone, canceledLabel);
  const label = person ? t('row.label', { lesson: lessonLabel, name: person }) : lessonLabel;

  return (
    <li
      data-canceled={canceled || undefined}
      data-past={past || undefined}
      className={cn(lessonCardSurface, 'flex flex-col sm:flex-row sm:items-center')}
    >
      <button
        type="button"
        aria-label={label}
        aria-describedby={detailsId}
        onClick={onOpen}
        className={cn(lessonCardButton, 'min-w-0 flex-1')}
      >
        <LessonCardBody
          lesson={lesson}
          timeZone={timeZone}
          detailsId={detailsId}
          quiet={past || canceled}
          withDate={withDate}
          today={today}
          person={person}
          canceledLabel={canceledLabel}
          badges={badges.map((b) => (
            <Badge key={b.label} variant={b.variant}>
              {b.label}
            </Badge>
          ))}
        />
      </button>
      {children}
    </li>
  );
}
