import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarX2, ListPlus, LogIn, X } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { LessonDetail, Me } from '@/api/client';
import type { ClosedWhy } from '@/api/conflict';
import { type BookedFor, familyQuery, meQuery, myPassesQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { PassHint } from '@/features/passes/PassHint';
import { formatDateTime, schoolDateOf } from '@/lib/schoolTime';
import { useServerClock } from '../useServerClock';
import {
  type Bookable,
  bookablePersons,
  closedState,
  unbookedPersons,
  useBookingWrites,
} from './bookingRules';
import { CancelBookingDialog } from './CancelBookingDialog';
import { CancelFollowingDialog } from './CancelFollowingDialog';
import { cancelIsLate } from './lateCancel';
import { WaitlistRow } from './WaitlistRow';

/**
 * The booking part of the lesson sheet: the free places, and for a
 * signed-in account the family's bookings with a cancel each, its entries
 * on the waiting list (position, or the invite to accept or decline) and a
 * way to book itself or a dependent, or to join the waiting list of a full
 * lesson. Visitors get a link to sign in.
 */
export function BookingSection({
  lesson,
  timeZone,
  label,
}: {
  lesson: LessonDetail;
  timeZone: string;
  /** The lesson in words (useLessonLabel), for the dialogs. */
  label: string;
}) {
  const { t } = useTranslation('schedule');
  const me = useQuery(meQuery);
  const headingId = useId();
  const now = useServerClock();

  return (
    <section aria-labelledby={headingId} className="space-y-3 rounded-lg border p-3">
      <h3 id={headingId} className="font-medium">
        {t('booking.title')}
      </h3>
      {me.data ? (
        <MemberBooking lesson={lesson} timeZone={timeZone} label={label} me={me.data} now={now} />
      ) : (
        <>
          <Availability lesson={lesson} timeZone={timeZone} closed={closedState(lesson, now)} />
          {me.data === null && (
            <Button asChild variant="outline" size="sm">
              <Link to="/login">
                <LogIn aria-hidden="true" />
                {t('booking.signIn')}
              </Link>
            </Button>
          )}
        </>
      )}
    </section>
  );
}

/** "3 places left" and until when, "This lesson is full." or why it is closed. */
function Availability({
  lesson,
  timeZone,
  closed,
}: {
  lesson: LessonDetail;
  timeZone: string;
  closed: ClosedWhy | undefined;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  if (closed) {
    return (
      <p className="flex items-center gap-1.5 text-sm font-medium">
        <CalendarX2 className="size-4 shrink-0" aria-hidden="true" />
        {t(`booking.closed.${closed}`)}
      </p>
    );
  }
  if (lesson.freePlaces === 0) return <p className="text-sm font-medium">{t('booking.full')}</p>;
  return (
    <p className="text-sm">
      <span className="font-medium">{t('booking.placesLeft', { count: lesson.freePlaces })}</span>
      <span className="block text-muted-foreground">
        {t('booking.until', { at: formatDateTime(lesson.bookingClosesAt, timeZone, lang) })}
      </span>
    </p>
  );
}

function MemberBooking({
  lesson,
  timeZone,
  label,
  me,
  now,
}: {
  lesson: LessonDetail;
  timeZone: string;
  label: string;
  me: Me;
  now: number;
}) {
  const { t } = useTranslation('schedule');
  const family = useQuery(familyQuery);
  const { book, join, refused, bookFor, joinFor } = useBookingWrites(lesson.id);
  const passes = useQuery(myPassesQuery);
  const selectId = useId();
  const stayId = useId();
  const hintId = useId();
  const [chosen, setChosen] = useState<string>();
  // "Stay on the list" as chosen for one person; off for everyone else.
  const [stay, setStay] = useState<{ personId: string; checked: boolean }>();
  const [canceling, setCanceling] = useState<BookedFor | null>(null);
  const [following, setFollowing] = useState<BookedFor | null>(null);

  const started = now >= Date.parse(lesson.startsAt);
  // A refusal for this lesson stays until the sheet shows another one.
  const closed = closedState(lesson, now) ?? refused;

  const persons = bookablePersons(me, family.data?.dependents);
  const booked = lesson.bookedFor ?? [];
  const waitlist = lesson.waitlist ?? [];
  const unbooked = unbookedPersons(persons, lesson, now);
  const target = unbooked.find((p) => p.id === chosen) ?? unbooked[0];
  const canBook = !closed && lesson.freePlaces > 0 && !book.isPending;
  // A full lesson that could be booked otherwise has a waiting list. While a
  // booking runs, its optimistic place does not turn Book into Join.
  const canJoin = !closed && lesson.freePlaces === 0 && !book.isPending;
  const stayOnList = stay !== undefined && stay.personId === target?.id && stay.checked;
  // Whether to say which pass the booking would use: while the lesson can
  // be booked and the family's passes are known.
  const showHint = !closed && lesson.freePlaces > 0 && target !== undefined && !!passes.data;
  const nameOf = (p: Bookable) => (p.self ? t('booking.you', { name: p.name }) : p.name);

  // A closed lesson says so in the section; everything else in a toast.
  const onBook = () => {
    if (target) bookFor(target);
  };

  const onJoin = () => {
    if (target) joinFor(target, stayOnList, () => setStay(undefined));
  };

  return (
    <>
      <Availability lesson={lesson} timeZone={timeZone} closed={closed} />

      {booked.length > 0 && (
        <ul className="space-y-2">
          {booked.map((b) => (
            <li key={b.personId} className="flex flex-wrap items-center gap-2">
              <span className="min-w-0 font-medium break-words">{b.name}</span>
              <Badge>{t('booking.booked')}</Badge>
              {!started && (
                <span className="flex w-full flex-wrap gap-2 sm:ml-auto sm:w-auto">
                  <Button
                    requiresOnline
                    variant="outline"
                    size="sm"
                    disabled={b.bookingId === ''}
                    aria-label={t('booking.cancelFor', { name: b.name })}
                    onClick={() => setCanceling(b)}
                  >
                    <X aria-hidden="true" />
                    {t('booking.cancel')}
                  </Button>
                  {lesson.classId && (
                    <Button
                      requiresOnline
                      variant="ghost"
                      size="sm"
                      disabled={b.bookingId === ''}
                      aria-label={t('booking.cancelFollowingFor', { name: b.name })}
                      onClick={() => setFollowing(b)}
                    >
                      {t('booking.cancelFollowing')}
                    </Button>
                  )}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}

      {waitlist.length > 0 && (
        <ul className="space-y-2">
          {waitlist.map((w) => (
            <WaitlistRow
              key={w.entryId}
              entry={w}
              self={w.personId === me.personId}
              active={!started && lesson.status !== 'canceled'}
              closed={now >= Date.parse(lesson.bookingClosesAt)}
              timeZone={timeZone}
              now={now}
            />
          ))}
        </ul>
      )}

      {target && !started && lesson.status !== 'canceled' && (
        <div className="flex flex-wrap items-end gap-2">
          {persons.length > 1 && (
            <div className="grid min-w-0 flex-1 gap-1.5">
              <Label htmlFor={selectId}>
                {canJoin ? t('booking.waitlist.for') : t('booking.for')}
              </Label>
              <Select value={target.id} onValueChange={setChosen}>
                <SelectTrigger id={selectId} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {unbooked.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {nameOf(p)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          {canJoin ? (
            <Button requiresOnline disabled={join.isPending} onClick={onJoin}>
              <ListPlus aria-hidden="true" />
              {join.isPending ? t('booking.waitlist.joining') : t('booking.waitlist.join')}
            </Button>
          ) : (
            <Button
              requiresOnline
              disabled={!canBook}
              aria-describedby={showHint ? hintId : undefined}
              onClick={onBook}
            >
              {book.isPending ? t('booking.booking') : t('booking.book')}
            </Button>
          )}
          {showHint && passes.data && (
            <PassHint
              id={hintId}
              passes={passes.data}
              personId={target.id}
              lessonId={lesson.id}
              date={schoolDateOf(lesson.startsAt, timeZone)}
            />
          )}
          {canJoin && (
            <div className="flex w-full items-center gap-2">
              <Checkbox
                id={stayId}
                checked={stayOnList}
                onCheckedChange={(checked) =>
                  setStay({ personId: target.id, checked: checked === true })
                }
              />
              <Label htmlFor={stayId} className="font-normal">
                {t('booking.waitlist.stayOnList')}
              </Label>
            </div>
          )}
        </div>
      )}

      <CancelBookingDialog
        key={canceling?.bookingId ?? 'none'}
        booking={canceling}
        lessonId={lesson.id}
        label={label}
        late={cancelIsLate(lesson, now)}
        onClose={() => setCanceling(null)}
      />
      <CancelFollowingDialog
        key={following?.bookingId ?? 'none'}
        booking={following}
        timeZone={timeZone}
        onClose={() => setFollowing(null)}
      />
    </>
  );
}
