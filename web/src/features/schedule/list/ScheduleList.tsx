import { CalendarClock, X } from 'lucide-react';
import { useId, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { Schedule, ScheduleLesson } from '@/api/client';
import { Button } from '@/components/ui/button';
import { addDays, formatDateTime, todayIn } from '@/lib/schoolTime';
import { useServerNow } from '@/lib/useNow';
import { cn } from '@/lib/utils';
import { AvailabilityBadges, hasAvailabilityBadges } from '../booking/AvailabilityBadges';
import { DayHeading } from './DayHeading';
import { LessonCardBody } from './LessonCard';
import { lessonCardButton, lessonCardSurface, useLessonCardLabel } from './cardParts';
import { type Day as DayOf, type DayTone, dayTone, groupByDay } from './days';
import { clearedFilters, hasActiveFilter } from '../filters/filterParams';
import { useScheduleSearch } from '../useScheduleSearch';

type Day = DayOf<ScheduleLesson>;

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
  onOpen,
}: {
  day: Day;
  tone: DayTone;
  now: number;
  timeZone: string;
  onOpen: (lessonId: string) => void;
}) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <DayHeading id={headingId} date={day.date} tone={tone} />
      <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {day.lessons.map((lesson) => (
          <li key={lesson.id} className="flex">
            <LessonCard
              lesson={lesson}
              past={Date.parse(lesson.endsAt) <= now}
              now={now}
              timeZone={timeZone}
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
  now,
  timeZone,
  onOpen,
}: {
  lesson: ScheduleLesson;
  past: boolean;
  /** The server's clock, in ms. */
  now: number;
  timeZone: string;
  onOpen: () => void;
}) {
  const detailsId = useId();
  const availabilityId = useId();
  const label = useLessonCardLabel()(lesson, timeZone);
  const canceled = lesson.status === 'canceled';
  // Canceled and ended lessons lose their accent colors.
  const quiet = canceled || past;

  return (
    <button
      type="button"
      aria-label={label}
      aria-describedby={
        hasAvailabilityBadges(lesson, !quiet, now) ? `${detailsId} ${availabilityId}` : detailsId
      }
      onClick={onOpen}
      data-canceled={canceled || undefined}
      data-past={past || undefined}
      className={cn(
        lessonCardSurface,
        lessonCardButton,
        'w-full transition-[background-color,box-shadow] hover:ring-primary/30',
      )}
    >
      <LessonCardBody lesson={lesson} timeZone={timeZone} detailsId={detailsId} quiet={quiet}>
        <AvailabilityBadges id={availabilityId} lesson={lesson} showFree={!quiet} now={now} />
      </LessonCardBody>
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
