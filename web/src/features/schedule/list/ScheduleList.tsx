import { CalendarClock, X } from 'lucide-react';
import { useId, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { Schedule, ScheduleLesson } from '@/api/client';
import { Button } from '@/components/ui/button';
import { useLessonQuickAction } from '@/features/schedule/booking/quickAction';
import { addDays, formatDateTime, todayIn } from '@/lib/schoolTime';
import { useServerNow } from '@/lib/useNow';
import type { ScheduleDay } from '@/themes/slots';
import { useSlot } from '@/themes/useSlot';
import { dayTone, groupByDay } from './days';
import { clearedFilters, hasActiveFilter } from '../filters/filterParams';
import { useScheduleSearch } from '../useScheduleSearch';

/**
 * The schedule as a feed of lesson cards grouped by school day, each day
 * under a heading colored by its distance from today (the ScheduleList,
 * DayHeading and LessonCard slots). Lessons that have ended are muted. Both
 * follow the server's clock (`schedule.now`, as of `receivedAt`), checked
 * once a minute. Days without lessons are left out.
 */
export function ScheduleList({ schedule, receivedAt }: { schedule: Schedule; receivedAt: number }) {
  const { i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const { update } = useScheduleSearch();
  const now = useServerNow(schedule.now, receivedAt);
  const today = todayIn(schedule.timezone, now);
  const tomorrow = addDays(today, 1);
  const idPrefix = useId();
  const List = useSlot('ScheduleList');
  const grouped = useMemo(
    () => groupByDay(schedule.lessons, schedule.timezone),
    [schedule.lessons, schedule.timezone],
  );
  const days: ScheduleDay[] = grouped.map((day) => ({
    ...day,
    tone: dayTone(day.date, today, tomorrow),
    headingId: `${idPrefix}${day.date}`,
  }));
  const onOpen = (lessonId: string) => void update({ lesson: lessonId });

  return (
    // The slot changes only with the theme, and then remounting what it renders is intended.
    // eslint-disable-next-line react-hooks/static-components
    <List
      days={days}
      timeZone={schedule.timezone}
      now={now}
      empty={<EmptySchedule schedule={schedule} lang={lang} />}
      onOpen={onOpen}
      renderLesson={(lesson) => (
        <ScheduleLessonCard
          lesson={lesson}
          timeZone={schedule.timezone}
          now={now}
          onOpen={() => onOpen(lesson.id)}
        />
      )}
    />
  );
}

/** A lesson's card (the LessonCard slot) with its quick action and that action's dialog. */
export function ScheduleLessonCard({
  lesson,
  timeZone,
  now,
  onOpen,
}: {
  lesson: ScheduleLesson;
  timeZone: string;
  /** The server's clock, in ms. */
  now: number;
  onOpen: () => void;
}) {
  const LessonCard = useSlot('LessonCard');
  const { action, dialog } = useLessonQuickAction(lesson, timeZone, now);
  return (
    <>
      {/* The slot changes only with the theme, and then remounting what it renders is intended. */}
      {/* eslint-disable-next-line react-hooks/static-components */}
      <LessonCard
        lesson={lesson}
        timeZone={timeZone}
        now={now}
        past={Date.parse(lesson.endsAt) <= now}
        quickAction={action}
        onOpen={onOpen}
      />
      {dialog}
    </>
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
