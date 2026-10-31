import { useQuery } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { useId, useMemo } from 'react';
import type { ScheduleLesson } from '@/api/client';
import { publicSettingsQuery, scheduleQuery } from '@/api/queries';
import { useMe } from '@/api/useMe';
import { dayTone, groupByDay } from '@/features/schedule/list/days';
import { ScheduleLessonCard } from '@/features/schedule/list/ScheduleList';
import { addDays, schoolDateOf, todayIn } from '@/lib/schoolTime';
import { useNow } from '@/lib/useNow';
import type { HomeMyLessons } from '@/themes/slots';

/** How many days from today the visitor's own lessons cover: the next lessons' week. */
export const myLessonsDays = 7;

/**
 * The signed-in visitor's own lessons of the coming week for the home page:
 * the lessons of the published schedule that they or a dependent are booked
 * for (`bookedFor`) and that have not ended, canceled ones included, by
 * school day. The same schedule query as the next lessons' teaser, so a
 * theme that shows both asks once. A lesson opens in the schedule.
 */
export function useMyLessons(): HomeMyLessons {
  const navigate = useNavigate();
  const idPrefix = useId();
  const me = useMe();
  const settings = useQuery(publicSettingsQuery);
  const signedIn = Boolean(me.data);
  const timeZone = settings.data?.timezone;
  const today = timeZone ? todayIn(timeZone) : undefined;
  const schedule = useQuery({
    ...scheduleQuery(
      today ? { from: today, to: addDays(today, myLessonsDays) } : { from: '', to: '' },
    ),
    enabled: signedIn && today !== undefined,
  });
  const clock = useNow();
  const lessons = schedule.data?.lessons;
  const zone = schedule.data?.timezone;
  const booked = useMemo(
    () => (lessons ?? []).filter((l) => (l.bookedFor?.length ?? 0) > 0),
    [lessons],
  );
  const grouped = useMemo(() => (zone ? groupByDay(booked, zone) : []), [booked, zone]);

  if (me.isError) return { state: 'error', onRetry: () => void me.refetch() };
  if (!me.isSuccess) return { state: 'unknown' };
  if (!me.data) return { state: 'signedOut' };
  if (settings.isError) return { state: 'error', onRetry: () => void settings.refetch() };
  if (schedule.isError) return { state: 'error', onRetry: () => void schedule.refetch() };
  if (!schedule.data || !zone) return { state: 'loading' };

  // The server's clock, ticking like the schedule's.
  const now = clock + Date.parse(schedule.data.now) - schedule.dataUpdatedAt;
  const serverToday = todayIn(zone, now);
  const tomorrow = addDays(serverToday, 1);
  const days = grouped
    .map((day) => ({
      ...day,
      lessons: day.lessons.filter((l) => Date.parse(l.endsAt) > now),
      tone: dayTone(day.date, serverToday, tomorrow),
      headingId: `${idPrefix}${day.date}`,
    }))
    .filter((day) => day.lessons.length > 0);

  const open = (lesson: ScheduleLesson) =>
    void navigate({
      to: '/schedule',
      search: { from: schoolDateOf(lesson.startsAt, zone), lesson: lesson.id },
    });

  return {
    state: 'ready',
    days,
    renderLesson: (lesson) => (
      <ScheduleLessonCard lesson={lesson} timeZone={zone} now={now} onOpen={() => open(lesson)} />
    ),
  };
}
