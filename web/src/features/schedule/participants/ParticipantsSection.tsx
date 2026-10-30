import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleAlert } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { LessonDetail } from '@/api/client';
import {
  ApiError,
  type AttendanceStatus,
  type LessonParticipant,
  type LessonParticipants,
  lessonParticipantsQuery,
  markAllAttendedMutation,
  meQuery,
  removeLessonParticipantMutation,
  setAttendanceMutation,
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
import { Button } from '@/components/ui/button';
import { canAdminister } from '@/features/admin/access';
import { shownName } from '@/lib/personName';
import type { ParticipantDialog } from '@/themes/slots';
import { useSlot } from '@/themes/useSlot';
import { useServerClock } from '../useServerClock';
import { AddParticipantDialog } from './AddParticipantDialog';
import { BookingHistoryDialog } from './BookingHistoryDialog';
import { useParticipantErrorText } from './participantErrors';
import { TrialDialog } from './TrialDialog';

/** What the section needs of a lesson. */
export type ParticipantsLesson = Pick<
  LessonDetail,
  'id' | 'startsAt' | 'endsAt' | 'status' | 'courseType'
>;

const isNotFound = (error: unknown) => error instanceof ApiError && error.status === 404;

/**
 * The participants of a lesson for staff (ADR 0009, amendment): the counts,
 * the bookings with their markers and contacts, the waiting list, and what
 * staff may do: add, remove, mark a trial, take attendance (from the start;
 * for instructors and accompanists until the end of the lesson's school day,
 * for the office at any time), and for the office a booking's history. The
 * API decides who sees it: the office every lesson, instructors and
 * accompanists their own; for any other lesson it answers 404 and the
 * section stays away. The theme's Participants slot renders it.
 */
export function ParticipantsSection({
  lesson,
  timeZone,
  label,
}: {
  lesson: ParticipantsLesson;
  timeZone: string;
  /** The lesson in words (useLessonLabel), for the dialogs. */
  label: string;
}) {
  const me = useQuery(meQuery);
  const role = me.data?.role;
  const staff = role === 'instructor' || canAdminister(role);
  const participants = useQuery({ ...lessonParticipantsQuery(lesson.id), enabled: staff });
  const headingId = useId();
  const Participants = useSlot('Participants');
  const onRetry = () => void participants.refetch();

  if (!staff || isNotFound(participants.error)) return null;
  // A failed refetch keeps the list (and its open dialogs); only a first
  // load that fails shows the error.
  if (participants.isError && !participants.data) {
    // The slot changes only with the theme, and then remounting what it renders is intended.
    // eslint-disable-next-line react-hooks/static-components
    return <Participants state="error" headingId={headingId} onRetry={onRetry} />;
  }
  if (!participants.data) return null;

  return (
    <LoadedParticipants
      lesson={lesson}
      label={label}
      data={participants.data}
      stale={participants.isError}
      onRetry={onRetry}
      office={canAdminister(role)}
      timeZone={timeZone}
      headingId={headingId}
    />
  );
}

/**
 * The state of a dialog about a target (a participant): whether it is open,
 * and the last target it was opened for, which it keeps while it closes so
 * its content stays during the animation. `seq` counts the openings: as a
 * key, it gives every opening a fresh dialog, but not the closing.
 */
function useDialogTarget<T>() {
  const [state, setState] = useState<{ target: T | null; open: boolean; seq: number }>({
    target: null,
    open: false,
    seq: 0,
  });
  return {
    ...state,
    show: (target: T) => setState((s) => ({ target, open: true, seq: s.seq + 1 })),
    hide: () => setState((s) => ({ ...s, open: false })),
  };
}

/**
 * Takes attendance with one mutation for all rows. It keeps the bookings
 * whose request runs, so marking one row leaves the others usable and two
 * rows can be marked at once. Each mark toasts its own outcome, but not once
 * the section is gone (as a row's own mutation would not).
 */
function useAttendance() {
  const { t } = useTranslation('schedule');
  const queryClient = useQueryClient();
  const errorText = useParticipantErrorText();
  const attendance = useMutation(setAttendanceMutation(queryClient));
  const [pending, setPending] = useState<ReadonlySet<string>>(() => new Set());
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const mark = (p: LessonParticipant, status: AttendanceStatus) => {
    const { bookingId } = p;
    const name = shownName(p);
    setPending((s) => new Set(s).add(bookingId));
    attendance
      .mutateAsync({ bookingId, status })
      .then(
        () => {
          if (!mounted.current) return;
          toast.success(
            status === 'attended'
              ? t('participants.attendance.attendedToast', { name })
              : status === 'no_show'
                ? t('participants.attendance.noShowToast', { name })
                : t('participants.attendance.resetToast', { name }),
          );
        },
        (error: unknown) => {
          if (mounted.current) toast.error(errorText(error, { name }));
        },
      )
      .finally(() =>
        setPending((s) => {
          const next = new Set(s);
          next.delete(bookingId);
          return next;
        }),
      );
  };

  return { mark, marking: (bookingId: string) => pending.has(bookingId) };
}

/** The participants once loaded: works out what the caller may do and connects the slot. */
function LoadedParticipants({
  lesson,
  label,
  data,
  stale,
  onRetry,
  office,
  timeZone,
  headingId,
}: {
  lesson: ParticipantsLesson;
  label: string;
  data: LessonParticipants;
  /** Whether the last refetch failed, so `data` may be out of date. */
  stale: boolean;
  onRetry: () => void;
  office: boolean;
  timeZone: string;
  headingId: string;
}) {
  const { t } = useTranslation('schedule');
  const Participants = useSlot('Participants');
  const queryClient = useQueryClient();
  const errorText = useParticipantErrorText();
  const allAttended = useMutation(markAllAttendedMutation(queryClient));
  const attendance = useAttendance();
  const now = useServerClock();
  const adding = useDialogTarget<true>();
  const trial = useDialogTarget<LessonParticipant>();
  const removing = useDialogTarget<LessonParticipant>();
  const history = useDialogTarget<LessonParticipant>();
  const open = (p: LessonParticipant, kind: ParticipantDialog) =>
    (kind === 'trial' ? trial : kind === 'remove' ? removing : history).show(p);

  const started = now >= Date.parse(lesson.startsAt);
  const dayOver = now >= Date.parse(data.dayOverAt);
  const canceledLesson = lesson.status === 'canceled';
  // Instructors and accompanists change participants until the end of the
  // lesson's school day; the office always.
  const canEdit = office || !dayOver;
  const canAdd = canEdit && !canceledLesson;
  const canAttend = started && canEdit && !canceledLesson;

  const active = data.bookings.filter((b) => b.status !== 'canceled');
  const canceled = data.bookings.filter((b) => b.status === 'canceled');

  const onAllAttended = () =>
    allAttended.mutate(
      { lessonId: lesson.id },
      {
        onSuccess: (changed) =>
          toast.success(
            changed.length > 0
              ? t('participants.attendance.allDone', { count: changed.length })
              : t('participants.attendance.allNone'),
          ),
        onError: (error) => toast.error(errorText(error, { name: '' })),
      },
    );

  return (
    // The slot changes only with the theme, and then remounting what it renders is intended.
    // eslint-disable-next-line react-hooks/static-components
    <Participants
      state="ready"
      headingId={headingId}
      counts={data}
      stale={stale}
      onRetry={onRetry}
      timeZone={timeZone}
      canEdit={canEdit}
      canAdd={canAdd}
      canAttend={canAttend}
      office={office}
      active={active}
      canceled={canceled}
      waitlist={data.waitlist}
      onAdd={() => adding.show(true)}
      allAttended={{
        show: canAttend && active.some((b) => b.status === 'booked'),
        pending: allAttended.isPending,
        run: onAllAttended,
      }}
      mark={attendance.mark}
      marking={attendance.marking}
      open={open}
      dialogs={
        <>
          <AddParticipantDialog
            key={`add-${adding.seq}`}
            open={adding.open}
            lessonId={lesson.id}
            label={label}
            onClose={adding.hide}
          />
          <TrialDialog
            key={`trial-${trial.seq}`}
            open={trial.open}
            participant={trial.target}
            onClose={trial.hide}
          />
          <RemoveDialog
            key={`remove-${removing.seq}`}
            open={removing.open}
            participant={removing.target}
            label={label}
            mail={!started && !canceledLesson}
            started={started}
            onClose={removing.hide}
          />
          {office && (
            <BookingHistoryDialog
              open={history.open}
              participant={history.target}
              timeZone={timeZone}
              onClose={history.hide}
            />
          )}
        </>
      }
    />
  );
}

/**
 * Asks before taking a participant off the lesson; a refusal keeps it open
 * and says why. `mail` says whether the participant hears of it: only
 * before the start of a lesson that takes place.
 */
function RemoveDialog({
  open,
  participant,
  label,
  mail,
  started,
  onClose,
}: {
  open: boolean;
  participant: LessonParticipant | null;
  label: string;
  mail: boolean;
  started: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation('schedule');
  const queryClient = useQueryClient();
  const errorText = useParticipantErrorText();
  const remove = useMutation(removeLessonParticipantMutation(queryClient));
  const name = participant ? shownName(participant) : '';

  const onConfirm = () => {
    if (!participant) return;
    remove.mutate(
      { bookingId: participant.bookingId },
      {
        onSuccess: () => {
          toast.success(t('participants.remove.done', { name }));
          onClose();
        },
      },
    );
  };

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !remove.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('participants.remove.title', { name })}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('participants.remove.description', { name, lesson: label })}{' '}
            {mail
              ? t('participants.remove.mail')
              : started
                ? t('participants.remove.noMailStarted')
                : t('participants.remove.noMailCanceled')}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {remove.error && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{errorText(remove.error, { name })}</AlertTitle>
          </Alert>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={remove.isPending}>
            {t('participants.remove.keep')}
          </AlertDialogCancel>
          {/* No AlertDialogAction: it would close the dialog before the answer. */}
          <Button variant="destructive" disabled={remove.isPending} onClick={onConfirm}>
            {remove.isPending
              ? t('participants.remove.removing')
              : t('participants.remove.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
