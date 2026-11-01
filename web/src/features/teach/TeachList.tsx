import { Check, ChevronDown, CircleAlert, Clock, Mail, MapPin, UserRound } from 'lucide-react';
import { useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TeachLesson, TeachLessons } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { useLessonLabel, useLessonTitle } from '@/features/schedule/lessonLabel';
import {
  type Day as DayOf,
  type DayTone,
  dayTone,
  groupByDay,
} from '@/features/schedule/list/days';
import { useServerClock } from '@/features/schedule/useServerClock';
import { VisibilityBadge } from '@/features/schedule/visibility/VisibilityBadge';
import { addDays, formatTimeRange, todayIn } from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import { useSlot } from '@/themes/useSlot';
import { type AttendanceState, attendanceState } from './attendance';
import type { NotifyTarget } from './NotifyDialog';

type Day = DayOf<TeachLesson>;

/**
 * The lessons of the instructor view by school day, today's heading in its
 * color. In a range that contains today, the days before it fold away under
 * "Earlier" (which says how many of them lack attendance). The attendance
 * states follow the server's clock (as the latest answer in the cache has
 * it, so the previous range shown while the next one loads keeps it too);
 * the office and admins (`office`) may take attendance at any time.
 */
export function TeachList({
  data,
  office,
  stale,
  onOpen,
  onNotify,
}: {
  data: TeachLessons;
  office: boolean;
  /** Whether the list is still the previous range's while the next one loads. */
  stale: boolean;
  onOpen: (lessonId: string) => void;
  onNotify: (target: NotifyTarget) => void;
}) {
  const { t, i18n } = useTranslation('teach');
  const lang = i18n.resolvedLanguage ?? 'en';
  const now = useServerClock();
  const today = todayIn(data.timezone, now);
  const tomorrow = addDays(today, 1);
  const [showEarlier, setShowEarlier] = useState(false);
  const earlierId = useId();
  const todayId = useId();
  const DayHeading = useSlot('DayHeading');
  const days = useMemo(
    () => groupByDay(data.lessons, data.timezone),
    [data.lessons, data.timezone],
  );

  if (days.length === 0) {
    return (
      <p className="rounded-xl border border-dashed px-4 py-10 text-center text-muted-foreground">
        {t('empty')}
      </p>
    );
  }

  // Fold the past away only where today is in the range: a past range is shown as it is.
  const foldPast = data.from <= today && today < data.to;
  const earlier = foldPast ? days.filter((d) => d.date < today) : [];
  const later = foldPast ? days.filter((d) => d.date >= today) : days;
  const earlierLessons = earlier.flatMap((d) => d.lessons);
  const missing = earlierLessons.filter(
    (l) => attendanceState(l, now, office) === 'missing',
  ).length;

  const renderDay = (day: Day) => (
    <DaySection
      key={day.date}
      day={day}
      tone={dayTone(day.date, today, tomorrow)}
      now={now}
      office={office}
      timeZone={data.timezone}
      lang={lang}
      onOpen={onOpen}
      onNotify={onNotify}
    />
  );

  return (
    <div className={cn('space-y-6', stale && 'opacity-60 transition-opacity')} aria-busy={stale}>
      {earlierLessons.length > 0 && (
        <div className="space-y-6">
          <Button
            variant="outline"
            className="h-auto w-full justify-between py-2 text-left whitespace-normal"
            aria-expanded={showEarlier}
            aria-controls={earlierId}
            onClick={() => setShowEarlier((s) => !s)}
          >
            <span>
              {t('earlier.toggle', { count: earlierLessons.length })}
              {missing > 0 && (
                <>
                  {' · '}
                  <span className="text-destructive">
                    {t('earlier.missing', { count: missing })}
                  </span>
                </>
              )}
            </span>
            <ChevronDown
              aria-hidden="true"
              className={cn('transition-transform', showEarlier && 'rotate-180')}
            />
          </Button>
          <div id={earlierId} hidden={!showEarlier} className="space-y-6">
            {showEarlier && earlier.map(renderDay)}
          </div>
        </div>
      )}
      {foldPast && later[0]?.date !== today && (
        <section aria-labelledby={todayId} className="space-y-3">
          {/* The slot changes only with the theme, and then remounting what it renders is intended. */}
          {/* eslint-disable-next-line react-hooks/static-components */}
          <DayHeading id={todayId} date={today} tone="today" />
          <p className="px-4 text-muted-foreground">{t('freeToday')}</p>
        </section>
      )}
      {later.map(renderDay)}
      {foldPast && later.length === 0 && <p className="text-muted-foreground">{t('emptyAhead')}</p>}
    </div>
  );
}

function DaySection({
  day,
  tone,
  now,
  office,
  timeZone,
  lang,
  onOpen,
  onNotify,
}: {
  day: Day;
  tone: DayTone;
  now: number;
  office: boolean;
  timeZone: string;
  lang: string;
  onOpen: (lessonId: string) => void;
  onNotify: (target: NotifyTarget) => void;
}) {
  const headingId = useId();
  const DayHeading = useSlot('DayHeading');
  return (
    <section aria-labelledby={headingId} className="space-y-3">
      {/* The slot changes only with the theme, and then remounting what it renders is intended. */}
      {/* eslint-disable-next-line react-hooks/static-components */}
      <DayHeading id={headingId} date={day.date} tone={tone} />
      <ul className="space-y-3">
        {day.lessons.map((lesson) => (
          <LessonRow
            key={lesson.id}
            lesson={lesson}
            attendance={attendanceState(lesson, now, office)}
            past={Date.parse(lesson.endsAt) <= now}
            timeZone={timeZone}
            lang={lang}
            onOpen={() => onOpen(lesson.id)}
            onNotify={onNotify}
          />
        ))}
      </ul>
    </section>
  );
}

const attendanceBadge: Record<
  AttendanceState,
  { variant: 'secondary' | 'destructive' | 'default'; Icon: typeof Check }
> = {
  open: { variant: 'default', Icon: Clock },
  taken: { variant: 'secondary', Icon: Check },
  missing: { variant: 'destructive', Icon: CircleAlert },
};

function LessonRow({
  lesson,
  attendance,
  past,
  timeZone,
  lang,
  onOpen,
  onNotify,
}: {
  lesson: TeachLesson;
  attendance: AttendanceState | undefined;
  past: boolean;
  timeZone: string;
  lang: string;
  onOpen: () => void;
  onNotify: (target: NotifyTarget) => void;
}) {
  const { t } = useTranslation('teach');
  const { t: tSchedule } = useTranslation('schedule');
  const detailsId = useId();
  const lessonLabel = useLessonLabel()(lesson, timeZone);
  const title = useLessonTitle();
  const canceled = lesson.status === 'canceled';
  const quiet = canceled || past;
  const special = lesson.kind !== 'regular';
  const label = [
    lessonLabel,
    canceled && tSchedule('status.canceled'),
    special && tSchedule(`kind.${lesson.kind}`),
    lesson.visibility !== 'public' && tSchedule(`visibility.${lesson.visibility}`),
  ]
    .filter(Boolean)
    .join(', ');
  const counts = [
    t('counts.booked', { active: lesson.active, capacity: lesson.capacity }),
    lesson.waiting > 0 && t('counts.waiting', { count: lesson.waiting }),
    lesson.trials > 0 && t('counts.trials', { count: lesson.trials }),
    lesson.guests > 0 && t('counts.guests', { count: lesson.guests }),
  ].filter(Boolean);
  const badge = attendance && attendanceBadge[attendance];

  // The thin primary line on the left is a barre; ended and canceled lessons lose it.
  return (
    <li
      data-canceled={canceled || undefined}
      data-past={past || undefined}
      className="flex flex-col gap-2 rounded-lg bg-card text-sm text-card-foreground shadow-[inset_2px_0_0_0_var(--color-primary)] ring-1 ring-foreground/10 data-canceled:bg-muted/40 data-canceled:shadow-none data-past:bg-muted/40 data-past:shadow-none sm:flex-row sm:items-center"
    >
      <button
        type="button"
        aria-label={label}
        aria-describedby={detailsId}
        onClick={onOpen}
        className="flex min-w-0 flex-1 flex-col gap-1.5 rounded-lg p-4 pl-5 text-left transition-colors outline-none hover:bg-accent/50 focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span
            className={cn(
              'inline-flex items-center gap-1.5 font-medium tabular-nums',
              canceled && 'line-through',
              !quiet && 'text-primary',
            )}
          >
            <Clock className="size-4" aria-hidden="true" />
            {formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang)}
          </span>
          <span
            className={cn(
              'font-heading text-lg leading-snug font-medium',
              canceled && 'text-muted-foreground line-through decoration-1',
            )}
          >
            {title(lesson)}
          </span>
        </span>
        <span id={detailsId} className="flex flex-col gap-1.5">
          <span className="flex flex-wrap gap-x-4 gap-y-1 text-muted-foreground">
            <span className="inline-flex items-start gap-1.5">
              <MapPin className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              <span>
                <span className="sr-only">{tSchedule('list.place')}: </span>
                {lesson.room.name} · {lesson.location.name}
              </span>
            </span>
            <span className="inline-flex items-start gap-1.5">
              <UserRound className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
              <span>
                <span className="sr-only">{tSchedule('list.instructor')}: </span>
                {lesson.instructor?.displayName}
                {lesson.accompanist && (
                  <>
                    {' · '}
                    {tSchedule('list.accompaniedBy', { name: lesson.accompanist.displayName })}
                  </>
                )}
              </span>
            </span>
          </span>
          <span className="flex flex-wrap items-center gap-1.5">
            {lesson.role !== 'office' && (
              <Badge variant="outline">{t(`role.${lesson.role}`)}</Badge>
            )}
            {canceled && <Badge variant="destructive">{tSchedule('status.canceled')}</Badge>}
            {special && <Badge variant="outline">{tSchedule(`kind.${lesson.kind}`)}</Badge>}
            <VisibilityBadge visibility={lesson.visibility} />
            {badge && attendance && (
              <Badge variant={badge.variant}>
                <badge.Icon aria-hidden="true" />
                {t(`attendance.${attendance}`)}
              </Badge>
            )}
          </span>
          <span className="text-muted-foreground tabular-nums">{counts.join(' · ')}</span>
        </span>
      </button>
      {lesson.active > 0 && (
        <div className="px-4 pb-4 sm:py-4 sm:pr-4 sm:pl-0">
          <Button
            variant="outline"
            size="sm"
            className="w-full sm:w-auto"
            aria-label={t('notify.openFor', { lesson: lessonLabel })}
            onClick={() => onNotify({ lessonId: lesson.id, label: lessonLabel })}
          >
            <Mail aria-hidden="true" />
            {t('notify.open')}
          </Button>
        </div>
      )}
    </li>
  );
}
