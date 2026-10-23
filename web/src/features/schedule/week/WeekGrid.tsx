import { ChevronLeft, ChevronRight } from 'lucide-react';
import { type ReactNode, useEffect, useId, useMemo, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import type { LessonSummary, Schedule } from '@/api/client';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  addDays,
  formatDate,
  formatDayHeading,
  formatTimeRange,
  startOfWeek,
  type SchoolDate,
} from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import { useScheduleSearch } from '../useScheduleSearch';
import { VisibilityBadge } from '../visibility/VisibilityBadge';
import { layoutWeek, MIN_BLOCK_MINUTES, type Placement, type TimeAxis } from './layout';

/** Height of one hour on the time axis. */
const HOUR_REM = 5;

const rem = (minutes: number) => `${(minutes / 60) * HOUR_REM}rem`;

const hourLabel = (hour: number) => `${String(hour).padStart(2, '0')}:00`;

/**
 * The published lessons of one week as a time grid: one column per school
 * date, Monday to Sunday, rows by wall-clock time in the school time zone.
 * Below md the columns scroll horizontally, one day per screen.
 */
export function WeekGrid({
  schedule,
  weekStart,
  today,
}: {
  schedule: Schedule;
  weekStart: SchoolDate;
  /** Today in the school time zone, as the page uses it for its default date. */
  today: SchoolDate;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const { search, update } = useScheduleSearch();
  const id = useId();
  const scrollRef = useRef<HTMLDivElement>(null);

  const tz = schedule.timezone;
  const week = useMemo(
    () => layoutWeek(schedule.lessons, weekStart, tz),
    [schedule.lessons, weekStart, tz],
  );
  const isCurrentWeek = startOfWeek(today) === weekStart;
  const focusDate = isCurrentWeek ? today : weekStart;

  // On narrow screens, start at today's column (or Monday).
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || el.scrollWidth <= el.clientWidth) return;
    const column = el.querySelector<HTMLElement>(`[data-date="${focusDate}"]`);
    const gutter = el.querySelector<HTMLElement>('[data-gutter]');
    if (column) el.scrollLeft = column.offsetLeft - (gutter?.offsetWidth ?? 0);
  }, [focusDate]);

  const range = t('week.range', {
    start: formatDate(weekStart, lang, { day: 'numeric', month: 'short' }),
    end: formatDate(addDays(weekStart, 6), lang, {
      day: 'numeric',
      month: 'short',
      year: 'numeric',
    }),
  });
  const { startHour, endHour } = week.axis;
  const bodyHeight = rem((endHour - startHour) * 60);

  return (
    <section aria-labelledby={`${id}-range`} className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id={`${id}-range`} className="font-heading text-lg font-semibold">
          {range}
        </h2>
        <nav aria-label={t('week.navigation')} className="flex items-center gap-1">
          <Button
            variant="outline"
            size="icon"
            aria-label={t('week.previous')}
            onClick={() => void update({ from: addDays(weekStart, -7) })}
          >
            <ChevronLeft aria-hidden="true" />
          </Button>
          <Button
            variant="outline"
            disabled={isCurrentWeek}
            onClick={() => void update({ from: undefined })}
          >
            {t('week.current')}
          </Button>
          <Button
            variant="outline"
            size="icon"
            aria-label={t('week.next')}
            onClick={() => void update({ from: addDays(weekStart, 7) })}
          >
            <ChevronRight aria-hidden="true" />
          </Button>
        </nav>
      </div>

      {week.isEmpty && (
        <p className="rounded-lg bg-muted px-3 py-2 text-sm text-muted-foreground">
          {t('week.empty')}
        </p>
      )}

      <div
        ref={scrollRef}
        role="group"
        aria-label={t('week.label')}
        className="relative flex snap-x snap-mandatory scroll-pl-12 overflow-x-auto overscroll-x-contain rounded-lg border md:grid md:grid-cols-[3rem_repeat(7,minmax(0,1fr))] md:overflow-x-visible"
      >
        <TimeGutter axis={week.axis} height={bodyHeight} />
        {week.days.map(({ date, placements }) => (
          <DayColumn
            key={date}
            headingId={`${id}-${date}`}
            date={date}
            lang={lang}
            isToday={date === today}
            height={bodyHeight}
          >
            {placements.map((p) => (
              <LessonBlock
                key={p.item.id}
                placement={p}
                date={date}
                startHour={startHour}
                timeZone={tz}
                lang={lang}
                selected={search.lesson === p.item.id}
                onOpen={() => void update({ lesson: p.item.id })}
              />
            ))}
          </DayColumn>
        ))}
      </div>
    </section>
  );
}

function TimeGutter({ axis, height }: { axis: TimeAxis; height: string }) {
  const hours = Array.from(
    { length: axis.endHour - axis.startHour + 1 },
    (_, i) => axis.startHour + i,
  );
  return (
    <div
      data-gutter
      aria-hidden="true"
      className="sticky left-0 z-10 w-12 shrink-0 border-r bg-background pb-2"
    >
      <div className="h-12 border-b" />
      <div className="relative" style={{ height }}>
        {hours.map((hour) => (
          <span
            key={hour}
            className="absolute right-1.5 -translate-y-1/2 bg-background text-xs text-muted-foreground tabular-nums"
            style={{ top: rem((hour - axis.startHour) * 60) }}
          >
            {hourLabel(hour)}
          </span>
        ))}
      </div>
    </div>
  );
}

function DayColumn({
  headingId,
  date,
  lang,
  isToday,
  height,
  children,
}: {
  headingId: string;
  date: SchoolDate;
  lang: string;
  isToday: boolean;
  height: string;
  children: ReactNode;
}) {
  return (
    <section
      aria-labelledby={headingId}
      data-date={date}
      className={cn(
        'min-w-0 shrink-0 basis-[calc(100%-3rem)] snap-start border-r pb-2 last:border-r-0 md:basis-auto',
        isToday && 'bg-primary/5',
      )}
    >
      <h3
        id={headingId}
        aria-current={isToday ? 'date' : undefined}
        className={cn(
          'flex h-12 items-baseline justify-center gap-1 border-b pt-3 text-sm text-muted-foreground',
          isToday && 'font-semibold text-primary',
        )}
      >
        {formatDate(date, lang, { weekday: 'short' })}{' '}
        <span className={cn('text-lg font-medium text-foreground', isToday && 'text-primary')}>
          {formatDate(date, lang, { day: 'numeric' })}
        </span>
      </h3>
      <div
        className="relative border-b"
        style={{
          height,
          backgroundImage: 'linear-gradient(to bottom, var(--color-border) 1px, transparent 1px)',
          backgroundSize: `100% ${HOUR_REM}rem`,
        }}
      >
        {children}
      </div>
    </section>
  );
}

function LessonBlock({
  placement,
  date,
  startHour,
  timeZone,
  lang,
  selected,
  onOpen,
}: {
  placement: Placement<LessonSummary>;
  date: SchoolDate;
  startHour: number;
  timeZone: string;
  lang: string;
  selected: boolean;
  onOpen: () => void;
}) {
  const { t } = useTranslation('schedule');
  const { item: lesson, start, end, lane, lanes } = placement;
  const canceled = lesson.status === 'canceled';
  const workshop = lesson.kind === 'workshop';
  const time = formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang);
  const label = [
    lesson.courseType.name,
    formatDayHeading(date, lang),
    time,
    lesson.room.name,
    canceled && t('status.canceled'),
    workshop && t('kind.workshop'),
    lesson.visibility !== 'public' && t(`visibility.${lesson.visibility}`),
  ]
    .filter(Boolean)
    .join(', ');

  return (
    <button
      type="button"
      aria-label={label}
      aria-haspopup="dialog"
      data-start={start}
      data-lane={lane}
      data-lanes={lanes}
      onClick={onOpen}
      className={cn(
        'absolute flex flex-col items-start gap-0.5 overflow-hidden rounded-md border px-1.5 py-1 text-left text-xs transition-colors outline-none focus-visible:z-20 focus-visible:ring-3 focus-visible:ring-ring/50',
        canceled
          ? 'border-dashed bg-muted text-muted-foreground hover:bg-muted/80'
          : workshop
            ? 'border-secondary-foreground/20 bg-secondary text-secondary-foreground hover:bg-secondary/80'
            : 'border-primary/30 bg-primary/10 hover:bg-primary/15',
        selected && 'z-10 ring-2 ring-primary',
      )}
      style={{
        top: rem(start - startHour * 60),
        height: rem(Math.max(end - start, MIN_BLOCK_MINUTES)),
        left: `calc(${(lane * 100) / lanes}% + 2px)`,
        width: `calc(${100 / lanes}% - 4px)`,
      }}
    >
      <span className={cn('tabular-nums', canceled && 'line-through')}>{time}</span>
      <span className={cn('w-full truncate font-medium', canceled && 'line-through')}>
        {lesson.courseType.name}
      </span>
      <span className="w-full truncate text-muted-foreground">{lesson.room.name}</span>
      {canceled && <Badge variant="destructive">{t('status.canceled')}</Badge>}
      {workshop && <Badge variant="outline">{t('kind.workshop')}</Badge>}
      <VisibilityBadge visibility={lesson.visibility} />
    </button>
  );
}
