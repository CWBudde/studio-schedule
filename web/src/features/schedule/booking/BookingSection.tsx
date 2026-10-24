import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarX2, Check, CircleAlert, ListPlus, LogIn, TriangleAlert, X } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { LessonDetail, Me } from '@/api/client';
import { type ClosedWhy, closedWhy } from '@/api/conflict';
import {
  acceptWaitlistInviteMutation,
  type BookedFor,
  bookLessonMutation,
  cancelBookingMutation,
  declineWaitlistInviteMutation,
  familyQuery,
  joinWaitlistMutation,
  leaveWaitlistMutation,
  meQuery,
  type WaitlistFor,
} from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Badge } from '@/components/ui/badge';
import { fullName } from '@/features/account/personForm';
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
import { formatDateTime, formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';
import { useServerClock } from '../useServerClock';
import { useBookingErrorText } from './bookingErrors';
import { CancelFollowingDialog } from './CancelFollowingDialog';
import { inviteRanOut, isInvited, staysOnList } from './waitlist';

/** A person the signed-in account books for: itself or one of its dependents. */
type Bookable = { id: string; name: string; self: boolean };

/**
 * The booking part of the lesson sheet: the free places, and for a
 * signed-in account the family's bookings with a cancel each, its entries
 * on the waiting list (position, or the invite to accept or decline) and a
 * way to book itself or a dependent, or to join the waiting list of a full
 * lesson. Visitors get a link to sign in.
 */
export function BookingSection({ lesson, timeZone }: { lesson: LessonDetail; timeZone: string }) {
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
        <MemberBooking lesson={lesson} timeZone={timeZone} me={me.data} now={now} />
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

/**
 * Why the lesson cannot be booked, in the order the API checks: canceled,
 * started, past the booking cutoff. Only the API knows whether members may
 * book it yet (release); it says so with a 409.
 */
function closedState(lesson: LessonDetail, now: number): ClosedWhy | undefined {
  if (lesson.status === 'canceled') return 'canceled';
  if (now >= Date.parse(lesson.startsAt)) return 'started';
  if (now >= Date.parse(lesson.bookingClosesAt)) return 'cutoff';
  return undefined;
}

/** Whether canceling now is late: from the cancellation cutoff on (never for a canceled lesson). */
function cancelIsLate(lesson: LessonDetail, now: number): boolean {
  return lesson.lateCancelFrom !== undefined && now >= Date.parse(lesson.lateCancelFrom);
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
  me,
  now,
}: {
  lesson: LessonDetail;
  timeZone: string;
  me: Me;
  now: number;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const queryClient = useQueryClient();
  const errorText = useBookingErrorText();
  const family = useQuery(familyQuery);
  const book = useMutation(bookLessonMutation(queryClient));
  const join = useMutation(joinWaitlistMutation(queryClient));
  const selectId = useId();
  const stayId = useId();
  const [chosen, setChosen] = useState<string>();
  // "Stay on the list" as chosen for one person; off for everyone else.
  const [stay, setStay] = useState<{ personId: string; checked: boolean }>();
  const [canceling, setCanceling] = useState<BookedFor | null>(null);
  const [following, setFollowing] = useState<BookedFor | null>(null);

  const started = now >= Date.parse(lesson.startsAt);
  // A refusal for this lesson stays until the sheet shows another one.
  const refused =
    (book.variables?.lessonId === lesson.id ? closedWhy(book.error) : undefined) ??
    (join.variables?.lessonId === lesson.id ? closedWhy(join.error) : undefined);
  const closed = closedState(lesson, now) ?? refused;
  const label = t('list.cardLabel', {
    courseType: lesson.courseType.name,
    day: formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang),
    time: formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang),
  });

  const self: Bookable = { id: me.personId, name: fullName(me), self: true };
  const persons: Bookable[] = [
    self,
    ...(family.data?.dependents ?? []).map((d) => ({
      id: d.id,
      name: fullName(d),
      self: false,
    })),
  ];
  const booked = lesson.bookedFor ?? [];
  const waitlist = lesson.waitlist ?? [];
  // Whoever is booked or on the waiting list already is not offered; an
  // invite that ran out and ends does not count.
  const unbooked = persons.filter(
    (p) =>
      !booked.some((b) => b.personId === p.id) &&
      !waitlist.some((w) => w.personId === p.id && staysOnList(w, now)),
  );
  const target = unbooked.find((p) => p.id === chosen) ?? unbooked[0];
  const canBook = !closed && lesson.freePlaces > 0 && !book.isPending;
  // A full lesson that could be booked otherwise has a waiting list. While a
  // booking runs, its optimistic place does not turn Book into Join.
  const canJoin = !closed && lesson.freePlaces === 0 && !book.isPending;
  const stayOnList = stay !== undefined && stay.personId === target?.id && stay.checked;
  const nameOf = (p: Bookable) => (p.self ? t('booking.you', { name: p.name }) : p.name);

  const onBook = () => {
    if (!target) return;
    book.mutate(
      { lessonId: lesson.id, personId: target.id, name: target.name },
      {
        onSuccess: () =>
          toast.success(t('booking.bookedToast', { name: target.name }), {
            description: t('booking.mailHint'),
          }),
        onError: (error) => {
          // A closed lesson says so in the section; everything else in a toast.
          if (!closedWhy(error)) toast.error(errorText(error, target.name));
        },
      },
    );
  };

  const onJoin = () => {
    if (!target) return;
    join.mutate(
      { lessonId: lesson.id, personId: target.id, name: target.name, stayOnList },
      {
        onSuccess: (entry) => {
          setStay(undefined);
          toast.success(
            entry.position === undefined
              ? t('booking.waitlist.joinedToastPlain', { name: target.name })
              : t('booking.waitlist.joinedToast', {
                  name: target.name,
                  position: entry.position,
                }),
            { description: t('booking.waitlist.inviteHint') },
          );
        },
        onError: (error) => {
          if (!closedWhy(error)) toast.error(errorText(error, target.name));
        },
      },
    );
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
            <Button disabled={join.isPending} onClick={onJoin}>
              <ListPlus aria-hidden="true" />
              {join.isPending ? t('booking.waitlist.joining') : t('booking.waitlist.join')}
            </Button>
          ) : (
            <Button disabled={!canBook} onClick={onBook}>
              {book.isPending ? t('booking.booking') : t('booking.book')}
            </Button>
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

/**
 * A person of the family on the waiting list: their place in it and a way
 * to leave it, or the place their invite holds, until when, with Accept
 * (books them) and Decline. An invite whose expiry has passed by the
 * server's clock shows as expired, even before the API says so. `active`
 * is false once the lesson has started or is canceled: then there is
 * nothing to do.
 */
function WaitlistRow({
  entry,
  self,
  active,
  timeZone,
  now,
}: {
  entry: WaitlistFor;
  self: boolean;
  active: boolean;
  timeZone: string;
  now: number;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const queryClient = useQueryClient();
  const errorText = useBookingErrorText();
  const leave = useMutation(leaveWaitlistMutation(queryClient));
  const accept = useMutation(acceptWaitlistInviteMutation(queryClient));
  const decline = useMutation(declineWaitlistInviteMutation(queryClient));
  const pending = leave.isPending || accept.isPending || decline.isPending;
  const { entryId, name } = entry;
  const variables = { entryId, name };
  const onError = (error: Error) => toast.error(errorText(error, name));

  const expired = inviteRanOut(entry, now);
  const invited = isInvited(entry, now);

  const onLeave = () =>
    leave.mutate(variables, {
      onSuccess: () => toast.success(t('booking.waitlist.leftToast', { name })),
      onError,
    });
  const onAccept = () =>
    accept.mutate(variables, {
      onSuccess: () =>
        toast.success(t('booking.bookedToast', { name }), {
          description: t('booking.mailHint'),
        }),
      onError,
    });
  const onDecline = () =>
    decline.mutate(variables, {
      onSuccess: (after) =>
        toast.success(
          after.status === 'waiting' && after.position !== undefined
            ? t('booking.waitlist.declinedRequeued', { name, position: after.position })
            : t('booking.waitlist.declinedToast', { name }),
        ),
      onError,
    });

  let badge: string;
  let note: string | undefined;
  if (invited) {
    badge = t('booking.waitlist.invited');
    if (entry.expiresAt === undefined) {
      note = self
        ? t('booking.waitlist.reservedYouPlain')
        : t('booking.waitlist.reservedForPlain', { name });
    } else {
      const at = formatDateTime(entry.expiresAt, timeZone, lang);
      note = self
        ? t('booking.waitlist.reservedYou', { at })
        : t('booking.waitlist.reservedFor', { name, at });
    }
  } else if (expired) {
    badge = t('booking.waitlist.expired');
    note = entry.stayOnList
      ? t('booking.waitlist.expiredRequeue', { name })
      : t('booking.waitlist.expiredEnds');
  } else {
    badge =
      entry.position === undefined
        ? t('booking.waitlist.waiting')
        : t('booking.waitlist.position', { position: entry.position });
  }
  // Leaving makes sense while waiting, or after a missed invite that would requeue.
  const canLeave = active && !invited && (!expired || entry.stayOnList);

  return (
    <li className="flex flex-wrap items-center gap-2">
      <span className="min-w-0 font-medium break-words">{name}</span>
      <Badge variant={invited ? 'default' : 'outline'}>{badge}</Badge>
      {note && <p className="w-full text-sm text-muted-foreground">{note}</p>}
      {active && invited && (
        <span className="flex w-full flex-wrap gap-2">
          <Button
            size="sm"
            disabled={pending}
            aria-label={t('booking.waitlist.acceptFor', { name })}
            onClick={onAccept}
          >
            <Check aria-hidden="true" />
            {t('booking.waitlist.accept')}
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={pending}
            aria-label={t('booking.waitlist.declineFor', { name })}
            onClick={onDecline}
          >
            <X aria-hidden="true" />
            {t('booking.waitlist.decline')}
          </Button>
        </span>
      )}
      {canLeave && (
        <span className="flex w-full flex-wrap gap-2 sm:ml-auto sm:w-auto">
          <Button
            variant="outline"
            size="sm"
            disabled={pending}
            aria-label={t('booking.waitlist.leaveFor', { name })}
            onClick={onLeave}
          >
            <X aria-hidden="true" />
            {t('booking.waitlist.leave')}
          </Button>
        </span>
      )}
    </li>
  );
}

/** Asks before canceling a booking; a refusal keeps the dialog open and says why. */
function CancelBookingDialog({
  booking,
  lessonId,
  label,
  late,
  onClose,
}: {
  booking: BookedFor | null;
  lessonId: string;
  label: string;
  /** Whether canceling now counts as late, as far as the client can tell. */
  late: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation('schedule');
  const queryClient = useQueryClient();
  const errorText = useBookingErrorText();
  const cancel = useMutation(cancelBookingMutation(queryClient));
  const name = booking?.name ?? '';

  const onConfirm = () => {
    if (!booking) return;
    cancel.mutate(
      { bookingId: booking.bookingId, lessonId },
      {
        onSuccess: ({ late }) => {
          const title = t('booking.canceled', { name });
          if (late) toast.warning(title, { description: t('booking.late') });
          else toast.success(title);
          onClose();
        },
      },
    );
  };

  return (
    <AlertDialog
      open={booking !== null}
      onOpenChange={(open) => {
        if (!open && !cancel.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('booking.confirm.title')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('booking.confirm.description', { name, lesson: label })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {late && (
          <Alert>
            <TriangleAlert aria-hidden="true" />
            <AlertTitle>{t('booking.confirm.late')}</AlertTitle>
          </Alert>
        )}
        {cancel.error && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{errorText(cancel.error, name)}</AlertTitle>
          </Alert>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={cancel.isPending}>
            {t('booking.confirm.keep')}
          </AlertDialogCancel>
          {/* No AlertDialogAction: it would close the dialog before the answer. */}
          <Button variant="destructive" disabled={cancel.isPending} onClick={onConfirm}>
            {cancel.isPending ? t('booking.confirm.canceling') : t('booking.confirm.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
