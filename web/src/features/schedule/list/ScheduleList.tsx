import { CalendarClock, Clock, MapPin, UserRound, X } from 'lucide-react';
import { useId, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { Schedule, ScheduleLesson } from '@/api/client';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  addDays,
  formatDateTime,
  formatDayHeading,
  formatTimeRange,
  type SchoolDate,
  schoolDateOf,
  todayIn,
} from '@/lib/schoolTime';
import { useServerNow } from '@/lib/useNow';
import { cn } from '@/lib/utils';
import { AvailabilityBadges, hasAvailabilityBadges } from '../booking/AvailabilityBadges';
import { clearedFilters, hasActiveFilter } from '../filters/filterParams';
import { useScheduleSearch } from '../useScheduleSearch';
import { VisibilityBadge } from '../visibility/VisibilityBadge';

type Day = { date: SchoolDate; lessons: ScheduleLesson[] };

/** How a day heading is colored, by the day's distance from today. */
type DayTone = 'past' | 'today' | 'tomorrow' | 'later';

const dayToneClass: Record<DayTone, string> = {
  past: 'bg-muted text-foreground',
  today: 'bg-day-today text-day-today-foreground',
  tomorrow: 'bg-day-tomorrow text-day-tomorrow-foreground',
  later: 'bg-day-later text-day-later-foreground',
};

function dayTone(date: SchoolDate, today: SchoolDate, tomorrow: SchoolDate): DayTone {
  if (date < today) return 'past';
  if (date === today) return 'today';
  return date === tomorrow ? 'tomorrow' : 'later';
}

/** Groups lessons by the school date they start on, keeping their order. */
function groupByDay(lessons: ScheduleLesson[], timeZone: string): Day[] {
  const days = new Map<SchoolDate, ScheduleLesson[]>();
  for (const lesson of lessons) {
    const date = schoolDateOf(lesson.startsAt, timeZone);
    const list = days.get(date);
    if (list) list.push(lesson);
    else days.set(date, [lesson]);
  }
  return Array.from(days, ([date, list]) => ({ date, lessons: list }));
}

/**
 * The schedule as a feed of lesson cards grouped by school day, each day
 * under a sticky heading colored by its distance from today. Lessons that
 * have ended are muted. Both follow the server's clock (`schedule.now`, as
 * of `receivedAt`), checked once a minute. Days without lessons are left
 * out.
 */
export function ScheduleList({ schedule, receivedAt }: { schedule: Schedule; receivedAt: number }) {
  const { i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const { update } = useScheduleSearch();
  const now = useServerNow(schedule.now, receivedAt);
  const today = todayIn(schedule.timezone, now);
  const tomorrow = addDays(today, 1);
  const days = useMemo(
    () => groupByDay(schedule.lessons, schedule.timezone),
    [schedule.lessons, schedule.timezone],
  );

  if (days.length === 0) return <EmptySchedule schedule={schedule} lang={lang} />;

  return (
    <div className="space-y-6">
      {days.map((day) => (
        <DaySection
          key={day.date}
          day={day}
          tone={dayTone(day.date, today, tomorrow)}
          now={now}
          timeZone={schedule.timezone}
          lang={lang}
          onOpen={(lesson) => void update({ lesson })}
        />
      ))}
    </div>
  );
}

function DaySection({
  day,
  tone,
  now,
  timeZone,
  lang,
  onOpen,
}: {
  day: Day;
  tone: DayTone;
  now: number;
  timeZone: string;
  lang: string;
  onOpen: (lessonId: string) => void;
}) {
  const { t } = useTranslation('schedule');
  const headingId = useId();
  const heading = formatDayHeading(day.date, lang);
  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <h2
        id={headingId}
        aria-current={tone === 'today' ? 'date' : undefined}
        className={cn(
          'sticky top-0 z-10 rounded-md px-4 py-2 font-heading text-xl font-medium tracking-tight shadow-[0_6px_16px_-12px_oklch(0.3_0.08_350/0.5)]',
          dayToneClass[tone],
        )}
      >
        {tone === 'today' || tone === 'tomorrow' ? t(`list.${tone}`, { day: heading }) : heading}
      </h2>
      <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {day.lessons.map((lesson) => (
          <li key={lesson.id} className="flex">
            <LessonCard
              lesson={lesson}
              past={Date.parse(lesson.endsAt) <= now}
              dayLabel={heading}
              timeZone={timeZone}
              lang={lang}
              onOpen={() => onOpen(lesson.id)}
            />
          </li>
        ))}
      </ul>
    </section>
  );
}

function LessonCard({
  lesson,
  past,
  dayLabel,
  timeZone,
  lang,
  onOpen,
}: {
  lesson: ScheduleLesson;
  past: boolean;
  dayLabel: string;
  timeZone: string;
  lang: string;
  onOpen: () => void;
}) {
  const { t } = useTranslation('schedule');
  const detailsId = useId();
  const availabilityId = useId();
  const canceled = lesson.status === 'canceled';
  // Canceled and ended lessons lose their accent colors.
  const quiet = canceled || past;
  const special = lesson.kind !== 'regular';
  const time = formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang);
  const label = [
    t('list.cardLabel', { courseType: lesson.courseType.name, day: dayLabel, time }),
    canceled && t('status.canceled'),
    special && t(`kind.${lesson.kind}`),
    lesson.visibility !== 'public' && t(`visibility.${lesson.visibility}`),
  ]
    .filter(Boolean)
    .join(', ');

  // The thin primary line on the left is a barre; ended and canceled lessons lose it.
  return (
    <button
      type="button"
      aria-label={label}
      aria-describedby={
        hasAvailabilityBadges(lesson, !quiet) ? `${detailsId} ${availabilityId}` : detailsId
      }
      onClick={onOpen}
      data-canceled={canceled || undefined}
      data-past={past || undefined}
      className="flex w-full flex-col gap-2 rounded-lg bg-card p-4 pl-5 text-left text-sm text-card-foreground shadow-[inset_2px_0_0_0_var(--color-primary)] ring-1 ring-foreground/10 transition-[background-color,box-shadow] outline-none hover:bg-accent/50 hover:ring-primary/30 focus-visible:ring-3 focus-visible:ring-ring/50 data-canceled:bg-muted/40 data-canceled:text-muted-foreground data-canceled:shadow-none data-past:bg-muted/40 data-past:text-muted-foreground data-past:shadow-none"
    >
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
          {time}
        </span>
        {canceled && <Badge variant="destructive">{t('status.canceled')}</Badge>}
        {special && <Badge variant="outline">{t(`kind.${lesson.kind}`)}</Badge>}
        <VisibilityBadge visibility={lesson.visibility} />
      </span>

      <span id={detailsId} className="flex flex-col gap-1 text-muted-foreground">
        <span className="inline-flex items-start gap-1.5">
          <MapPin className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span>
            <span className="sr-only">{t('list.place')}: </span>
            {lesson.room.name} · {lesson.location.name}
          </span>
        </span>
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
      </span>
      <AvailabilityBadges id={availabilityId} lesson={lesson} showFree={!quiet} />
    </button>
  );
}

function EmptySchedule({ schedule, lang }: { schedule: Schedule; lang: string }) {
  const { t } = useTranslation('schedule');
  const { search, update } = useScheduleSearch();
  const filtered = hasActiveFilter(search);

  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-10 text-center">
      <CalendarClock className="size-8 text-muted-foreground" aria-hidden="true" />
      <p className="font-heading text-lg font-semibold">{t('list.empty.title')}</p>
      <p className="max-w-prose text-muted-foreground">
        {filtered ? t('list.empty.filtered') : t('list.empty.unpublished')}
      </p>
      <p className="text-sm text-muted-foreground">
        {t('list.empty.nextRelease', {
          date: formatDateTime(schedule.nextReleaseAt, schedule.timezone, lang),
        })}
      </p>
      {filtered && (
        <Button variant="outline" size="sm" onClick={() => void update(clearedFilters)}>
          <X aria-hidden="true" />
          {t('filters.reset')}
        </Button>
      )}
    </div>
  );
}
