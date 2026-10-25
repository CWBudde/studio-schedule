import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarSearch, ChevronDown, ListX, X } from 'lucide-react';
import { type ReactNode, useEffect, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Me } from '@/api/client';
import {
  type BookedFor,
  type MyLessonBooking,
  type MyLessons,
  myLessonsQuery,
  myLessonsQueryKey,
  type MyWaitlistEntry,
} from '@/api/queries';
import { Button } from '@/components/ui/button';
import { LoadError } from '@/features/admin/crud/LoadError';
import { CancelBookingDialog } from '@/features/schedule/booking/CancelBookingDialog';
import { CancelFollowingDialog } from '@/features/schedule/booking/CancelFollowingDialog';
import { cancelIsLate } from '@/features/schedule/booking/lateCancel';
import { WaitlistRow } from '@/features/schedule/booking/WaitlistRow';
import { useLessonLabel } from '@/features/schedule/lessonLabel';
import { dayTone, dayToneClass, groupByDay } from '@/features/schedule/list/days';
import { useServerClock, useServerClockOffset } from '@/features/schedule/useServerClock';
import { addDays, formatDayHeading, type SchoolDate, todayIn } from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import { Loading } from './Loading';
import { MyLessonRow, type RowBadge } from './MyLessonRow';
import { useNamesPersons } from './useNamesPersons';

/** How far ahead "Coming up" shows lessons before "Show later lessons". */
const upcomingDays = 28;

/** The longest wait for the next lesson to end before asking again anyway. */
const maxWait = 24 * 3600_000;

/**
 * "Waiting lists" and "Coming up", from one answer: the family's open
 * waiting-list entries with their position, or the invite to accept or
 * decline (the list is left out while empty); and its active bookings of
 * lessons that are not over, by school day, the next four weeks first,
 * each with a cancel (and, for a class lesson, the following weeks too)
 * while it has not started. By the server's clock, a lesson that ends
 * leaves "Coming up" and an entry whose lesson starts leaves its list; then
 * "My lessons" is asked again, so the lesson moves to the history.
 */
export function UpcomingSections({ me, onOpen }: { me: Me; onOpen: (lessonId: string) => void }) {
  const { t } = useTranslation('myLessons');
  const lessons = useQuery(myLessonsQuery);
  const now = useServerClock();
  const upcomingId = useId();
  const waitlistId = useId();
  useRefreshAtNextChange(lessons.data);

  const upcoming = (lessons.data?.upcoming ?? []).filter((i) => Date.parse(i.lesson.endsAt) > now);
  const waitlist = (lessons.data?.waitlist ?? []).filter(
    (i) => Date.parse(i.lesson.startsAt) > now,
  );

  return (
    <>
      {lessons.data && waitlist.length > 0 && (
        <section aria-labelledby={waitlistId} className="space-y-4">
          <h2 id={waitlistId} className="font-heading text-2xl font-medium tracking-tight">
            {t('waitlist.title')}
          </h2>
          <Waitlist
            entries={waitlist}
            timeZone={lessons.data.timezone}
            me={me}
            now={now}
            onOpen={onOpen}
          />
        </section>
      )}
      <section aria-labelledby={upcomingId} className="space-y-4">
        <h2 id={upcomingId} className="font-heading text-2xl font-medium tracking-tight">
          {t('upcoming.title')}
        </h2>
        {lessons.data ? (
          <Upcoming items={upcoming} timeZone={lessons.data.timezone} now={now} onOpen={onOpen} />
        ) : lessons.isError ? (
          <LoadError message={t('loadError')} onRetry={() => void lessons.refetch()} />
        ) : (
          <Loading />
        )}
      </section>
    </>
  );
}

/**
 * Asks "My lessons" (the coming lessons and the history) again when, by
 * the server's clock, the next of its lessons ends or the next lesson with
 * a waiting-list entry starts.
 */
function useRefreshAtNextChange(data: MyLessons | undefined) {
  const queryClient = useQueryClient();
  const offset = useServerClockOffset();
  const next = useMemo(() => {
    if (!data) return undefined;
    const now = Date.parse(data.now);
    const changes = [
      ...data.upcoming.map((i) => Date.parse(i.lesson.endsAt)),
      ...data.waitlist.map((i) => Date.parse(i.lesson.startsAt)),
    ].filter((at) => at > now);
    return changes.length > 0 ? Math.min(...changes) : undefined;
  }, [data]);

  useEffect(() => {
    if (next === undefined) return;
    const wait = Math.min(Math.max(next - (Date.now() + offset), 0) + 1000, maxWait);
    const timer = setTimeout(
      () => void queryClient.invalidateQueries({ queryKey: myLessonsQueryKey }),
      wait,
    );
    return () => clearTimeout(timer);
  }, [next, offset, queryClient]);
}

function Upcoming({
  items,
  timeZone,
  now,
  onOpen,
}: {
  items: MyLessonBooking[];
  timeZone: string;
  now: number;
  onOpen: (lessonId: string) => void;
}) {
  const { t } = useTranslation('myLessons');
  const named = useNamesPersons();
  const lessonLabel = useLessonLabel();
  const [canceling, setCanceling] = useState<MyLessonBooking | null>(null);
  const [following, setFollowing] = useState<BookedFor | null>(null);
  const [showLater, setShowLater] = useState(false);
  const laterId = useId();
  const days = useMemo(
    () =>
      groupByDay(
        items.map((item) => ({ ...item, startsAt: item.lesson.startsAt })),
        timeZone,
      ),
    [items, timeZone],
  );
  const today = todayIn(timeZone, now);

  if (days.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-8 text-center">
        <p className="text-muted-foreground">{t('upcoming.empty')}</p>
        <Button asChild variant="outline">
          <Link to="/schedule">
            <CalendarSearch aria-hidden="true" />
            {t('upcoming.toSchedule')}
          </Link>
        </Button>
      </div>
    );
  }

  const horizon = addDays(today, upcomingDays);
  const soon = days.filter((d) => d.date < horizon);
  const later = days.filter((d) => d.date >= horizon);
  const laterCount = later.reduce((n, d) => n + d.lessons.length, 0);

  const renderDay = (day: (typeof days)[number]) => (
    <DayGroup key={day.date} date={day.date} today={today}>
      {day.lessons.map((item) => (
        <UpcomingRow
          key={item.booking.id}
          item={item}
          timeZone={timeZone}
          today={today}
          now={now}
          named={named}
          onOpen={() => onOpen(item.lesson.id)}
          onCancel={() => setCanceling(item)}
          onCancelFollowing={setFollowing}
        />
      ))}
    </DayGroup>
  );

  return (
    <div className="space-y-5">
      {soon.length === 0 && <p className="text-muted-foreground">{t('upcoming.noneSoon')}</p>}
      {soon.map(renderDay)}
      {later.length > 0 && (
        <>
          {!showLater && (
            <Button
              variant="outline"
              className="w-full sm:w-auto"
              aria-expanded={false}
              aria-controls={laterId}
              onClick={() => setShowLater(true)}
            >
              <ChevronDown aria-hidden="true" />
              {t('upcoming.showLater', { count: laterCount })}
            </Button>
          )}
          <div id={laterId} hidden={!showLater} className="space-y-5">
            {showLater && later.map(renderDay)}
          </div>
        </>
      )}
      <CancelBookingDialog
        key={canceling?.booking.id ?? 'none'}
        booking={canceling && bookedFor(canceling)}
        lessonId={canceling?.lesson.id ?? ''}
        label={canceling ? lessonLabel(canceling.lesson, timeZone) : ''}
        late={canceling ? cancelIsLate(canceling, now) : false}
        onClose={() => setCanceling(null)}
      />
      <CancelFollowingDialog
        key={following?.bookingId ?? 'none'}
        booking={following}
        timeZone={timeZone}
        onClose={() => setFollowing(null)}
      />
    </div>
  );
}

/** A booking as the cancel dialogs name it. */
const bookedFor = ({ booking, personName }: MyLessonBooking): BookedFor => ({
  personId: booking.personId,
  name: personName,
  bookingId: booking.id,
});

/**
 * The lessons of one school day under a heading colored like the
 * schedule's; a day in another year than today has its year.
 */
function DayGroup({
  date,
  today,
  children,
}: {
  date: SchoolDate;
  today: SchoolDate;
  children: ReactNode;
}) {
  const { t, i18n } = useTranslation('schedule');
  const headingId = useId();
  const tone = dayTone(date, today, addDays(today, 1));
  const heading = formatDayHeading(date, i18n.resolvedLanguage ?? 'en', today);
  return (
    <div role="group" aria-labelledby={headingId} className="space-y-2">
      <h3
        id={headingId}
        aria-current={tone === 'today' ? 'date' : undefined}
        className={cn(
          'rounded-md px-4 py-1.5 font-heading text-lg font-medium tracking-tight',
          dayToneClass[tone],
        )}
      >
        {tone === 'today' || tone === 'tomorrow' ? t(`list.${tone}`, { day: heading }) : heading}
      </h3>
      <ul className="space-y-2">{children}</ul>
    </div>
  );
}

function UpcomingRow({
  item,
  timeZone,
  today,
  now,
  named,
  onOpen,
  onCancel,
  onCancelFollowing,
}: {
  item: MyLessonBooking;
  timeZone: string;
  today: SchoolDate;
  now: number;
  named: boolean;
  onOpen: () => void;
  onCancel: () => void;
  onCancelFollowing: (booking: BookedFor) => void;
}) {
  const { t } = useTranslation('myLessons');
  const { t: tSchedule } = useTranslation('schedule');
  const { lesson, personName } = item;
  const started = now >= Date.parse(lesson.startsAt);
  const canCancel = item.canCancel && !started;

  // A lesson the school canceled says so on its card; the booking stays, but means nothing.
  const badges: RowBadge[] =
    lesson.status === 'canceled'
      ? []
      : [
          { label: t('status.booked'), variant: 'default' },
          ...(started ? [{ label: t('status.now'), variant: 'secondary' } as const] : []),
        ];

  return (
    <MyLessonRow
      lesson={lesson}
      timeZone={timeZone}
      today={today}
      person={named ? personName : undefined}
      badges={badges}
      onOpen={onOpen}
    >
      {canCancel && (
        <div className="flex flex-wrap gap-2 px-4 pb-4 sm:flex-col sm:items-stretch sm:py-4 sm:pr-4 sm:pl-0">
          <Button
            variant="outline"
            size="sm"
            aria-label={tSchedule('booking.cancelFor', { name: personName })}
            onClick={onCancel}
          >
            <X aria-hidden="true" />
            {tSchedule('booking.cancel')}
          </Button>
          {lesson.classId && (
            <Button
              variant="ghost"
              size="sm"
              aria-label={tSchedule('booking.cancelFollowingFor', { name: personName })}
              onClick={() => onCancelFollowing(bookedFor(item))}
            >
              <ListX aria-hidden="true" />
              {tSchedule('booking.cancelFollowing')}
            </Button>
          )}
        </div>
      )}
    </MyLessonRow>
  );
}

function Waitlist({
  entries,
  timeZone,
  me,
  now,
  onOpen,
}: {
  entries: MyWaitlistEntry[];
  timeZone: string;
  me: Me;
  now: number;
  onOpen: (lessonId: string) => void;
}) {
  const named = useNamesPersons();
  const today = todayIn(timeZone, now);
  return (
    <ul className="space-y-2">
      {entries.map((item) => (
        <WaitlistItem
          key={item.entry.entryId}
          item={item}
          self={item.entry.personId === me.personId}
          timeZone={timeZone}
          today={today}
          now={now}
          named={named}
          onOpen={() => onOpen(item.lesson.id)}
        />
      ))}
    </ul>
  );
}

/**
 * An entry of the family on a waiting list, with its lesson. Once booking
 * has closed (`bookingClosesAt`) no invite can come: the entry says so
 * instead of its position, and may only be left.
 */
function WaitlistItem({
  item,
  self,
  timeZone,
  today,
  now,
  named,
  onOpen,
}: {
  item: MyWaitlistEntry;
  self: boolean;
  timeZone: string;
  today: SchoolDate;
  now: number;
  named: boolean;
  onOpen: () => void;
}) {
  const { lesson, entry } = item;
  const active = now < Date.parse(lesson.startsAt) && lesson.status !== 'canceled';
  return (
    <MyLessonRow
      lesson={lesson}
      timeZone={timeZone}
      today={today}
      withDate
      person={named ? entry.name : undefined}
      badges={[]}
      onOpen={onOpen}
    >
      <ul className="px-4 pb-4 sm:w-72 sm:shrink-0 sm:py-4 sm:pr-4 sm:pl-0">
        <WaitlistRow
          entry={entry}
          self={self}
          active={active}
          closed={now >= Date.parse(item.bookingClosesAt)}
          timeZone={timeZone}
          now={now}
        />
      </ul>
    </MyLessonRow>
  );
}
