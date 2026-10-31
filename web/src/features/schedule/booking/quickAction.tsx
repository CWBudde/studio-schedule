import { useQuery } from '@tanstack/react-query';
import { type ReactNode, useState } from 'react';
import type { ScheduleLesson } from '@/api/client';
import { type BookedFor, familyQuery, meQuery } from '@/api/queries';
import { useLessonLabel } from '../lessonLabel';
import { bookablePersons, closedState, useBookingWrites } from './bookingRules';
import { CancelBookingDialog } from './CancelBookingDialog';
import { cancelIsLate } from './lateCancel';
import { staysOnList } from './waitlist';

/**
 * What a lesson card's one-tap action does for the signed-in account
 * (Werkstatt's square button; Programme's cards only open the sheet).
 */
export type QuickAction =
  /** Signed out, ended or started, canceled, or closed for booking. */
  | { kind: 'none' }
  /**
   * More than one person to book for (or the family not known yet), or a
   * state only the sheet handles (on the waiting list): open the sheet.
   */
  | { kind: 'open' }
  | {
      kind: 'book' | 'cancel' | 'waitlist';
      /** The one person the action is for: the account's own. */
      personId: string;
      /** Books, joins the waiting list, or opens the cancel confirmation. */
      run(): void;
      /** The booking or joining runs, or the booking to cancel is not saved yet. */
      pending: boolean;
    }
  /**
   * Full, and no way to join a waiting list. useLessonQuickAction never
   * answers it: every full lesson that is open for booking has a waiting
   * list (the API has no setting to turn it off).
   */
  | { kind: 'full' };

const none: QuickAction = { kind: 'none' };
const open: QuickAction = { kind: 'open' };

/**
 * The quick action of a lesson card at `now` (the server's clock, in ms),
 * by the rules of the lesson sheet's booking section: for an account that
 * books for itself only, book, cancel or join the waiting list; with
 * dependents (or while the family loads) the sheet, so the action never
 * guesses whom it is for. `dialog` is the cancel confirmation (with the
 * late-cancel notice) that `run` of a `cancel` opens: render it next to the
 * card.
 *
 * Unlike the plan's `useLessonQuickAction(lesson)`, it takes the time zone
 * (for the dialog's lesson label) and the list's clock, and returns the
 * dialog, which a hook cannot render.
 */
export function useLessonQuickAction(
  lesson: ScheduleLesson,
  timeZone: string,
  now: number,
): { action: QuickAction; dialog: ReactNode } {
  const me = useQuery(meQuery);
  // A guest's request would answer 401, which signs out (sessionCaches).
  const family = useQuery({ ...familyQuery, enabled: !!me.data });
  // The card has no section to say why the API refused: a toast does.
  const { book, join, refused, bookFor, joinFor } = useBookingWrites(lesson.id, {
    toastClosed: true,
  });
  const lessonLabel = useLessonLabel();
  const [canceling, setCanceling] = useState<BookedFor | null>(null);

  const action = ((): QuickAction => {
    if (!me.data) return none;
    const closed = closedState(lesson, now);
    if (closed === 'canceled' || closed === 'started') return none;
    if (!family.data) return open;
    const persons = bookablePersons(me.data, family.data.dependents);
    const [person] = persons;
    if (persons.length > 1 || !person) return open;

    const booking = lesson.bookedFor?.find((b) => b.personId === person.id);
    if (booking) {
      return {
        kind: 'cancel',
        personId: person.id,
        run: () => setCanceling(booking),
        // As in the sheet: an optimistic booking cannot be canceled yet.
        pending: booking.bookingId === '',
      };
    }
    if (lesson.waitlist?.some((w) => w.personId === person.id && staysOnList(w, now))) {
      return open;
    }
    if (closed ?? refused) return none;
    if (lesson.freePlaces > 0) {
      return {
        kind: 'book',
        personId: person.id,
        run: () => bookFor(person),
        pending: book.isPending,
      };
    }
    return {
      kind: 'waitlist',
      personId: person.id,
      run: () => joinFor(person, false),
      pending: join.isPending,
    };
  })();

  // Mounted only while asking, as the sheet's dialog remounts per booking.
  const dialog = canceling && (
    <CancelBookingDialog
      booking={canceling}
      lessonId={lesson.id}
      label={lessonLabel(lesson, timeZone)}
      late={cancelIsLate(lesson, now)}
      onClose={() => setCanceling(null)}
    />
  );

  return { action, dialog };
}
