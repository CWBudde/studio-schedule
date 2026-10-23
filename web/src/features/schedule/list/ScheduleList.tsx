import { CalendarClock, Clock, MapPin, UserRound, X } from 'lucide-react';
import { useId, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { LessonSummary, Schedule } from '@/api/client';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  formatDateTime,
  formatDayHeading,
  formatTimeRange,
  type SchoolDate,
  schoolDateOf,
} from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import { clearedFilters, hasActiveFilter } from '../filters/filterParams';
import { useScheduleSearch } from '../useScheduleSearch';
import { VisibilityBadge } from '../visibility/VisibilityBadge';

type Day = { date: SchoolDate; lessons: LessonSummary[] };

/** Groups lessons by the school date they start on, keeping their order. */
function groupByDay(lessons: LessonSummary[], timeZone: string): Day[] {
  const days = new Map<SchoolDate, LessonSummary[]>();
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
 * under a sticky heading. Days without lessons are left out.
 */
export function ScheduleList({ schedule }: { schedule: Schedule }) {
  const { i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const { update } = useScheduleSearch();
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
  timeZone,
  lang,
  onOpen,
}: {
  day: Day;
  timeZone: string;
  lang: string;
  onOpen: (lessonId: string) => void;
}) {
  const headingId = useId();
  const heading = formatDayHeading(day.date, lang);
  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <h2
        id={headingId}
        className="sticky top-0 z-10 -mx-1 bg-background/90 px-1 py-2 font-heading text-lg font-semibold tracking-tight backdrop-blur supports-[backdrop-filter]:bg-background/75"
      >
        {heading}
      </h2>
      <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {day.lessons.map((lesson) => (
          <li key={lesson.id} className="flex">
            <LessonCard
              lesson={lesson}
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
  dayLabel,
  timeZone,
  lang,
  onOpen,
}: {
  lesson: LessonSummary;
  dayLabel: string;
  timeZone: string;
  lang: string;
  onOpen: () => void;
}) {
  const { t } = useTranslation('schedule');
  const detailsId = useId();
  const canceled = lesson.status === 'canceled';
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

  return (
    <button
      type="button"
      aria-label={label}
      aria-describedby={detailsId}
      onClick={onOpen}
      data-canceled={canceled || undefined}
      className="flex w-full flex-col gap-2 rounded-xl bg-card p-4 text-left text-sm text-card-foreground ring-1 ring-foreground/10 transition-colors outline-none hover:bg-accent/60 focus-visible:ring-3 focus-visible:ring-ring/50 data-canceled:bg-muted/40 data-canceled:text-muted-foreground"
    >
      <span className="flex flex-wrap items-center gap-2">
        <span
          className={cn(
            'inline-flex items-center gap-1.5 font-medium tabular-nums',
            canceled && 'line-through',
          )}
        >
          <Clock className="size-4 text-muted-foreground" aria-hidden="true" />
          {time}
        </span>
        {canceled && <Badge variant="destructive">{t('status.canceled')}</Badge>}
        {special && <Badge variant="outline">{t(`kind.${lesson.kind}`)}</Badge>}
        <VisibilityBadge visibility={lesson.visibility} />
      </span>

      <span className="flex flex-wrap items-center gap-2">
        <span
          className={cn(
            'font-heading text-base font-semibold',
            canceled && 'line-through decoration-2',
          )}
        >
          {lesson.courseType.name}
        </span>
        {lesson.level && <Badge variant="secondary">{t(`level.${lesson.level}`)}</Badge>}
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
            <span>{lesson.instructor.displayName}</span>
            {lesson.accompanist && (
              <>
                {' · '}
                <span>{t('list.accompaniedBy', { name: lesson.accompanist.displayName })}</span>
              </>
            )}
          </span>
        </span>
      </span>
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
