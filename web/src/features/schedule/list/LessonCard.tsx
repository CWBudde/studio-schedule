import { Clock, KeyRound, MapPin, UserRound, UsersRound } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import {
  formatDate,
  formatTimeRange,
  schoolDateOf,
  type SchoolDate,
  yearUnlessIn,
} from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import { VisibilityBadge } from '../visibility/VisibilityBadge';
import type { CardLesson } from './cardParts';

/**
 * The content of a lesson card, for inside its button: the course type and
 * level; the time (with the date for `withDate`, its year only outside the
 * year of `today`), canceled (`canceledLabel`), kind and visibility badges
 * and `badges`; the place, the staff and the `person` it is for, under
 * `detailsId` (for the button's aria-describedby); then `children`.
 * `quiet` (ended or canceled) drops the accent colors.
 */
export function LessonCardBody({
  lesson,
  timeZone,
  detailsId,
  quiet,
  withDate,
  today,
  person,
  canceledLabel,
  badges,
  children,
}: {
  lesson: CardLesson;
  timeZone: string;
  detailsId: string;
  quiet: boolean;
  withDate?: boolean;
  today?: SchoolDate;
  person?: string;
  canceledLabel?: string;
  badges?: ReactNode;
  children?: ReactNode;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const canceled = lesson.status === 'canceled';
  const date = schoolDateOf(lesson.startsAt, timeZone);

  return (
    <>
      <span className="flex items-baseline justify-between gap-3">
        <span
          className={cn(
            'font-heading text-lg leading-snug font-medium',
            canceled && 'line-through decoration-1',
          )}
        >
          {lesson.courseType.name}
        </span>
        {lesson.level && (
          <span className="shrink-0 text-right eyebrow text-muted-foreground">
            {t(`level.${lesson.level}`)}
          </span>
        )}
      </span>

      <span className="flex flex-wrap items-center gap-2">
        <span
          className={cn(
            'inline-flex items-center gap-1.5 font-medium tabular-nums',
            canceled && 'line-through',
            !quiet && 'text-primary',
          )}
        >
          <Clock className="size-4" aria-hidden="true" />
          {withDate && (
            <>
              {formatDate(date, lang, {
                weekday: 'short',
                day: 'numeric',
                month: 'short',
                ...yearUnlessIn(date, today),
              })}
              {' · '}
            </>
          )}
          {formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang)}
        </span>
        {canceled && <Badge variant="destructive">{canceledLabel ?? t('status.canceled')}</Badge>}
        {lesson.kind !== 'regular' && <Badge variant="outline">{t(`kind.${lesson.kind}`)}</Badge>}
        <VisibilityBadge visibility={lesson.visibility} />
        {badges}
      </span>

      <span id={detailsId} className="flex flex-col gap-1 text-muted-foreground">
        <span className="inline-flex items-start gap-1.5">
          <MapPin className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>
            <span className="sr-only">{t('list.place')}: </span>
            {lesson.room.name} · {lesson.location.name}
          </span>
        </span>
        {lesson.instructor ? (
          <span className="inline-flex items-start gap-1.5">
            <UserRound className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
            <span>
              <span className="sr-only">{t('list.instructor')}: </span>
              <span className={cn(!quiet && 'text-primary')}>{lesson.instructor.displayName}</span>
              {lesson.accompanist && (
                <>
                  {' · '}
                  <span>{t('list.accompaniedBy', { name: lesson.accompanist.displayName })}</span>
                </>
              )}
            </span>
          </span>
        ) : (
          lesson.renter && (
            // A room rental (staff only) has its renter instead of staff.
            <span className="inline-flex items-start gap-1.5">
              <KeyRound className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              <span>
                <span className="sr-only">{t('list.renter')}: </span>
                {lesson.renter}
              </span>
            </span>
          )
        )}
        {person && (
          <span className="inline-flex items-start gap-1.5 text-foreground">
            <UsersRound className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
            <span>
              <span className="sr-only">{t('list.for')}: </span>
              {person}
            </span>
          </span>
        )}
      </span>
      {children}
    </>
  );
}
