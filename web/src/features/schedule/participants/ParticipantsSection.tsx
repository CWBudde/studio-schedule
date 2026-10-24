import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Check,
  CheckCheck,
  CircleAlert,
  History,
  Mail,
  Phone,
  RotateCcw,
  UserMinus,
  UserPlus,
  UserX,
  FlaskConical,
} from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { LessonDetail } from '@/api/client';
import {
  ApiError,
  type AttendanceStatus,
  type LessonParticipant,
  type LessonParticipants,
  type LessonWaitlistEntry,
  lessonParticipantsQuery,
  markAllAttendedMutation,
  meQuery,
  removeLessonParticipantMutation,
  setAttendanceMutation,
} from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
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
import { Button } from '@/components/ui/button';
import { canAdminister } from '@/features/admin/access';
import { formatDateTime } from '@/lib/schoolTime';
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
 * section stays away.
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
  const { t } = useTranslation('schedule');
  const me = useQuery(meQuery);
  const role = me.data?.role;
  const staff = role === 'instructor' || canAdminister(role);
  const participants = useQuery({ ...lessonParticipantsQuery(lesson.id), enabled: staff });
  const headingId = useId();

  if (!staff || isNotFound(participants.error)) return null;
  // A failed refetch keeps the list (and its open dialogs); only a first
  // load that fails shows the error.
  if (participants.isError && !participants.data) {
    return (
      <section aria-labelledby={headingId} className="space-y-3 rounded-lg border p-3">
        <h3 id={headingId} className="font-medium">
          {t('participants.title')}
        </h3>
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{t('participants.loadError')}</AlertTitle>
          <AlertDescription>
            <Button
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => void participants.refetch()}
            >
              <RotateCcw aria-hidden="true" />
              {t('participants.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </section>
    );
  }
  if (!participants.data) return null;

  return (
    <Participants
      lesson={lesson}
      label={label}
      data={participants.data}
      stale={participants.isError}
      onRetry={() => void participants.refetch()}
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

function Participants({
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
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const queryClient = useQueryClient();
  const errorText = useParticipantErrorText();
  const allAttended = useMutation(markAllAttendedMutation(queryClient));
  const now = useServerClock();
  const adding = useDialogTarget<true>();
  const trial = useDialogTarget<LessonParticipant>();
  const removing = useDialogTarget<LessonParticipant>();
  const history = useDialogTarget<LessonParticipant>();
  const openFor = (p: LessonParticipant) => (kind: 'trial' | 'remove' | 'history') =>
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

  const counts = [
    t('participants.counts.booked', { active: data.active, capacity: data.capacity }),
    t('participants.counts.free', { count: data.freePlaces }),
    ...(data.held > 0 ? [t('participants.counts.held', { count: data.held })] : []),
    ...(data.waiting > 0 ? [t('participants.counts.waiting', { count: data.waiting })] : []),
  ];

  return (
    <section aria-labelledby={headingId} className="space-y-3 rounded-lg border p-3">
      <h3 id={headingId} className="font-medium">
        {t('participants.title')}
      </h3>
      <p className="text-sm text-muted-foreground">{counts.join(' · ')}</p>
      {stale && (
        <p role="status" className="flex flex-wrap items-center gap-2 text-sm">
          <CircleAlert className="size-4 shrink-0 text-destructive" aria-hidden="true" />
          {t('participants.stale')}
          <Button variant="outline" size="xs" onClick={onRetry}>
            <RotateCcw aria-hidden="true" />
            {t('participants.retry')}
          </Button>
        </p>
      )}
      {!canEdit && <p className="text-sm font-medium">{t('participants.dayOver')}</p>}

      {(canAdd || (canAttend && active.some((b) => b.status === 'booked'))) && (
        <div className="flex flex-wrap gap-2">
          {canAdd && (
            <Button size="sm" onClick={() => adding.show(true)}>
              <UserPlus aria-hidden="true" />
              {t('participants.add.open')}
            </Button>
          )}
          {canAttend && active.some((b) => b.status === 'booked') && (
            <Button
              variant="outline"
              size="sm"
              disabled={allAttended.isPending}
              onClick={onAllAttended}
            >
              <CheckCheck aria-hidden="true" />
              {t('participants.attendance.all')}
            </Button>
          )}
        </div>
      )}

      {active.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('participants.empty')}</p>
      ) : (
        <ul className="divide-y" aria-label={t('participants.listLabel')}>
          {active.map((p) => (
            <ParticipantRow
              key={p.bookingId}
              participant={p}
              canEdit={canEdit}
              canAttend={canAttend}
              office={office}
              onOpen={openFor(p)}
            />
          ))}
        </ul>
      )}

      {canceled.length > 0 && (
        <div className="space-y-1">
          <h4 className="text-sm font-medium">{t('participants.canceledHeading')}</h4>
          <ul className="divide-y" aria-label={t('participants.canceledHeading')}>
            {canceled.map((p) => (
              <ParticipantRow
                key={p.bookingId}
                participant={p}
                canEdit={false}
                canAttend={false}
                office={office}
                canceledAt={
                  p.canceledAt === undefined
                    ? undefined
                    : formatDateTime(p.canceledAt, timeZone, lang)
                }
                onOpen={openFor(p)}
              />
            ))}
          </ul>
        </div>
      )}

      {data.waitlist.length > 0 && (
        <div className="space-y-1">
          <h4 className="text-sm font-medium">{t('participants.waitlist.title')}</h4>
          <ul className="space-y-1.5" aria-label={t('participants.waitlist.title')}>
            {data.waitlist.map((w) => (
              <WaitlistRow key={w.entryId} entry={w} timeZone={timeZone} />
            ))}
          </ul>
        </div>
      )}

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
    </section>
  );
}

/** The badge of a booking's status. */
function StatusBadge({ status }: { status: LessonParticipant['status'] }) {
  const { t } = useTranslation('schedule');
  const variant =
    status === 'attended'
      ? 'default'
      : status === 'no_show'
        ? 'destructive'
        : status === 'canceled'
          ? 'secondary'
          : 'outline';
  return <Badge variant={variant}>{t(`participants.status.${status}`)}</Badge>;
}

/**
 * A booking of the lesson: the name, the guest and trial markers, the
 * status, the trial note and the contacts, and the actions staff may take
 * now.
 */
function ParticipantRow({
  participant: p,
  canEdit,
  canAttend,
  office,
  canceledAt,
  onOpen,
}: {
  participant: LessonParticipant;
  canEdit: boolean;
  canAttend: boolean;
  office: boolean;
  /** When a canceled booking was canceled, in words. */
  canceledAt?: string;
  onOpen: (kind: 'trial' | 'remove' | 'history') => void;
}) {
  const { t } = useTranslation('schedule');
  const queryClient = useQueryClient();
  const errorText = useParticipantErrorText();
  const attendance = useMutation(setAttendanceMutation(queryClient));
  const name = p.name;

  const mark = (status: AttendanceStatus) =>
    attendance.mutate(
      { bookingId: p.bookingId, status },
      {
        onSuccess: () =>
          toast.success(
            status === 'attended'
              ? t('participants.attendance.attendedToast', { name })
              : status === 'no_show'
                ? t('participants.attendance.noShowToast', { name })
                : t('participants.attendance.resetToast', { name }),
          ),
        onError: (error) => toast.error(errorText(error, { name })),
      },
    );

  return (
    <li className="space-y-1.5 py-2.5 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="min-w-0 font-medium break-words">{name}</span>
        {p.guest && <Badge variant="secondary">{t('participants.guest')}</Badge>}
        {p.trial && <Badge variant="secondary">{t('participants.trial.badge')}</Badge>}
        <StatusBadge status={p.status} />
        {p.late && <Badge variant="destructive">{t('participants.late')}</Badge>}
      </div>
      {p.trial && p.trialNote && (
        <p className="text-sm break-words text-muted-foreground">
          {t('participants.trial.note', { note: p.trialNote })}
        </p>
      )}
      {canceledAt && (
        <p className="text-xs text-muted-foreground">
          {t('participants.canceledAt', { at: canceledAt })}
        </p>
      )}
      {(p.contactEmail || p.contactPhone) && (
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm">
          {p.contactEmail && (
            <a
              href={`mailto:${p.contactEmail}`}
              className="inline-flex min-w-0 items-center gap-1 break-all text-primary underline-offset-4 hover:underline"
            >
              <Mail className="size-3.5 shrink-0" aria-hidden="true" />
              {p.contactEmail}
            </a>
          )}
          {p.contactPhone && (
            <a
              href={`tel:${p.contactPhone.replace(/[^\d+]/g, '')}`}
              className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline"
            >
              <Phone className="size-3.5 shrink-0" aria-hidden="true" />
              {p.contactPhone}
            </a>
          )}
        </div>
      )}
      {canAttend && (
        <div
          role="group"
          aria-label={t('participants.attendance.label', { name })}
          className="flex flex-wrap gap-2"
        >
          <Button
            variant={p.status === 'attended' ? 'default' : 'outline'}
            size="sm"
            aria-pressed={p.status === 'attended'}
            aria-label={t('participants.attendance.attendedFor', { name })}
            disabled={attendance.isPending}
            onClick={() => p.status !== 'attended' && mark('attended')}
          >
            <Check aria-hidden="true" />
            {t('participants.attendance.attended')}
          </Button>
          <Button
            variant={p.status === 'no_show' ? 'destructive' : 'outline'}
            size="sm"
            aria-pressed={p.status === 'no_show'}
            aria-label={t('participants.attendance.noShowFor', { name })}
            disabled={attendance.isPending}
            onClick={() => p.status !== 'no_show' && mark('no_show')}
          >
            <UserX aria-hidden="true" />
            {t('participants.attendance.noShow')}
          </Button>
          {p.status !== 'booked' && (
            <Button
              variant="ghost"
              size="sm"
              aria-label={t('participants.attendance.resetFor', { name })}
              disabled={attendance.isPending}
              onClick={() => mark('booked')}
            >
              <RotateCcw aria-hidden="true" />
              {t('participants.attendance.reset')}
            </Button>
          )}
        </div>
      )}
      {(canEdit || office) && (
        <div className="flex flex-wrap gap-2">
          {canEdit && (
            <>
              <Button
                variant="ghost"
                size="sm"
                aria-label={t('participants.trial.editFor', { name })}
                onClick={() => onOpen('trial')}
              >
                <FlaskConical aria-hidden="true" />
                {t('participants.trial.edit')}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                aria-label={t('participants.remove.for', { name })}
                onClick={() => onOpen('remove')}
              >
                <UserMinus aria-hidden="true" />
                {t('participants.remove.open')}
              </Button>
            </>
          )}
          {office && (
            <Button
              variant="ghost"
              size="sm"
              aria-label={t('participants.history.for', { name })}
              onClick={() => onOpen('history')}
            >
              <History aria-hidden="true" />
              {t('participants.history.open')}
            </Button>
          )}
        </div>
      )}
    </li>
  );
}

/** An entry of the waiting list: the name, its place or what became of it. */
function WaitlistRow({ entry, timeZone }: { entry: LessonWaitlistEntry; timeZone: string }) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  let badge: string;
  if (entry.status === 'waiting') {
    badge =
      entry.position === undefined
        ? t('participants.waitlist.waiting')
        : t('participants.waitlist.position', { position: entry.position });
  } else if (entry.status === 'invited') {
    badge =
      entry.expiresAt === undefined
        ? t('participants.waitlist.invitedPlain')
        : t('participants.waitlist.invited', {
            at: formatDateTime(entry.expiresAt, timeZone, lang),
          });
  } else {
    badge = t(`participants.waitlist.${entry.status}`);
  }
  const ended = entry.status !== 'waiting' && entry.status !== 'invited';
  return (
    <li className="flex flex-wrap items-center gap-1.5 text-sm">
      <span className={ended ? 'min-w-0 break-words text-muted-foreground' : 'min-w-0 break-words'}>
        {entry.name}
      </span>
      <Badge variant={entry.status === 'invited' ? 'default' : 'outline'}>{badge}</Badge>
    </li>
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
  const name = participant?.name ?? '';

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
