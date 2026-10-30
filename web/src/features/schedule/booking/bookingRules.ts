import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { LessonDetail, Me } from '@/api/client';
import { type ClosedWhy, closedWhy } from '@/api/conflict';
import { bookLessonMutation, joinWaitlistMutation } from '@/api/queries';
import { fullName } from '@/features/account/personForm';
import { useBookingErrorText } from './bookingErrors';
import { staysOnList } from './waitlist';

/** A person the signed-in account books for: itself or one of its dependents. */
export type Bookable = { id: string; name: string; self: boolean };

/** What the booking rules need of a lesson (the schedule's lessons and the lesson sheet's have it). */
type BookingLesson = Pick<
  LessonDetail,
  'status' | 'startsAt' | 'bookingClosesAt' | 'bookedFor' | 'waitlist'
>;

/**
 * Why the lesson cannot be booked, in the order the API checks: canceled,
 * started, past the booking cutoff. Only the API knows whether members may
 * book it yet (release); it says so with a 409.
 */
export function closedState(lesson: BookingLesson, now: number): ClosedWhy | undefined {
  if (lesson.status === 'canceled') return 'canceled';
  if (now >= Date.parse(lesson.startsAt)) return 'started';
  if (now >= Date.parse(lesson.bookingClosesAt)) return 'cutoff';
  return undefined;
}

/** The persons the account books for: itself first, then its dependents. */
export function bookablePersons(
  me: Me,
  dependents: readonly { id: string; givenName: string; familyName: string }[] = [],
): Bookable[] {
  return [
    { id: me.personId, name: fullName(me), self: true },
    ...dependents.map((d) => ({ id: d.id, name: fullName(d), self: false })),
  ];
}

/**
 * The persons who may still book the lesson or join its waiting list:
 * whoever is booked or on the waiting list already is not offered; an
 * invite that ran out and ends (by `now`, the server's clock) does not
 * count.
 */
export function unbookedPersons(
  persons: readonly Bookable[],
  lesson: BookingLesson,
  now: number,
): Bookable[] {
  const booked = lesson.bookedFor ?? [];
  const waitlist = lesson.waitlist ?? [];
  return persons.filter(
    (p) =>
      !booked.some((b) => b.personId === p.id) &&
      !waitlist.some((w) => w.personId === p.id && staysOnList(w, now)),
  );
}

/**
 * Booking a lesson and joining its waiting list for a person, with the
 * toasts of the lesson sheet: who is booked or on the list, or why it
 * failed. `refused` is why the API refused a write for `lessonId` as closed;
 * it stays until another lesson is asked for. Such a refusal gets no toast
 * (the sheet says so in its section) unless `toastClosed`.
 */
export function useBookingWrites(lessonId: string, { toastClosed = false } = {}) {
  const { t } = useTranslation('schedule');
  const queryClient = useQueryClient();
  const errorText = useBookingErrorText();
  const book = useMutation(bookLessonMutation(queryClient));
  const join = useMutation(joinWaitlistMutation(queryClient));

  const refused =
    (book.variables?.lessonId === lessonId ? closedWhy(book.error) : undefined) ??
    (join.variables?.lessonId === lessonId ? closedWhy(join.error) : undefined);

  const failed = (name: string) => (error: Error) => {
    if (toastClosed || !closedWhy(error)) toast.error(errorText(error, name));
  };

  const bookFor = (target: Bookable) =>
    book.mutate(
      { lessonId, personId: target.id, name: target.name },
      {
        onSuccess: () =>
          toast.success(t('booking.bookedToast', { name: target.name }), {
            description: t('booking.mailHint'),
          }),
        onError: failed(target.name),
      },
    );

  /** Joins the waiting list; `onJoined` runs once that succeeded. */
  const joinFor = (target: Bookable, stayOnList: boolean, onJoined?: () => void) =>
    join.mutate(
      { lessonId, personId: target.id, name: target.name, stayOnList },
      {
        onSuccess: (entry) => {
          onJoined?.();
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
        onError: failed(target.name),
      },
    );

  return { book, join, refused, bookFor, joinFor };
}
