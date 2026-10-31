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
import { useTranslation } from 'react-i18next';
import type { AttendanceStatus, LessonParticipant, LessonWaitlistEntry } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { PaymentBadge } from '@/features/passes/PassBits';
import { shownName } from '@/lib/personName';
import { formatDateTime } from '@/lib/schoolTime';
import type { ParticipantDialog, ParticipantsProps, ParticipantsReadyProps } from '../slots';

/**
 * Programme's participants section: the counts, the bookings as a list with
 * their markers, contacts and actions, the canceled bookings, the waiting
 * list, and the dialogs; or the error of a first load that failed.
 */
export function Participants(props: ParticipantsProps) {
  const { t } = useTranslation('schedule');
  if (props.state === 'ready') return <ParticipantsList {...props} />;
  return (
    <section aria-labelledby={props.headingId} className="space-y-3 rounded-lg border p-3">
      <h3 id={props.headingId} className="font-medium">
        {t('participants.title')}
      </h3>
      <Alert variant="destructive">
        <CircleAlert aria-hidden="true" />
        <AlertTitle>{t('participants.loadError')}</AlertTitle>
        <AlertDescription>
          <Button variant="outline" size="sm" className="mt-2" onClick={props.onRetry}>
            <RotateCcw aria-hidden="true" />
            {t('participants.retry')}
          </Button>
        </AlertDescription>
      </Alert>
    </section>
  );
}

function ParticipantsList({
  headingId,
  counts: data,
  stale,
  onRetry,
  timeZone,
  canEdit,
  canAdd,
  canAttend,
  office,
  active,
  canceled,
  waitlist,
  onAdd,
  allAttended,
  mark,
  marking,
  open,
  dialogs,
}: ParticipantsReadyProps) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';

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

      {(canAdd || allAttended.show) && (
        <div className="flex flex-wrap gap-2">
          {canAdd && (
            <Button size="sm" onClick={onAdd}>
              <UserPlus aria-hidden="true" />
              {t('participants.add.open')}
            </Button>
          )}
          {allAttended.show && (
            <Button
              variant="outline"
              size="sm"
              disabled={allAttended.pending}
              onClick={allAttended.run}
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
              marking={marking(p.bookingId)}
              onMark={(status) => mark(p, status)}
              onOpen={(dialog) => open(p, dialog)}
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
                marking={marking(p.bookingId)}
                onMark={(status) => mark(p, status)}
                onOpen={(dialog) => open(p, dialog)}
              />
            ))}
          </ul>
        </div>
      )}

      {waitlist.length > 0 && (
        <div className="space-y-1">
          <h4 className="text-sm font-medium">{t('participants.waitlist.title')}</h4>
          <ul className="space-y-1.5" aria-label={t('participants.waitlist.title')}>
            {waitlist.map((w) => (
              <WaitlistRow key={w.entryId} entry={w} timeZone={timeZone} />
            ))}
          </ul>
        </div>
      )}

      {dialogs}
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
  marking,
  onMark: mark,
  onOpen,
}: {
  participant: LessonParticipant;
  canEdit: boolean;
  canAttend: boolean;
  office: boolean;
  /** When a canceled booking was canceled, in words. */
  canceledAt?: string;
  /** Whether this booking's attendance is being recorded. */
  marking: boolean;
  onMark: (status: AttendanceStatus) => void;
  onOpen: (dialog: ParticipantDialog) => void;
}) {
  const { t } = useTranslation('schedule');
  const name = shownName(p);

  return (
    <li className="space-y-1.5 py-2.5 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="min-w-0 font-medium break-words">{name}</span>
        {p.guest && <Badge variant="secondary">{t('participants.guest')}</Badge>}
        {p.trial && <Badge variant="secondary">{t('participants.trial.badge')}</Badge>}
        <StatusBadge status={p.status} />
        {p.late && <Badge variant="destructive">{t('participants.late')}</Badge>}
        <PaymentBadge payment={p.payment} />
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
            disabled={marking}
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
            disabled={marking}
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
              disabled={marking}
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
        {shownName(entry)}
      </span>
      <Badge variant={entry.status === 'invited' ? 'default' : 'outline'}>{badge}</Badge>
    </li>
  );
}
