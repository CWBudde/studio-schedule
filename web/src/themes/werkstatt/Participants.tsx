import {
  CheckCheck,
  CircleAlert,
  FlaskConical,
  History,
  RotateCcw,
  UserMinus,
  UserPlus,
  UserX,
} from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { AttendanceStatus, LessonParticipant, LessonWaitlistEntry } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { PaymentBadge } from '@/features/passes/PassBits';
import { shownName } from '@/lib/personName';
import { formatDateTime } from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import type { ParticipantsProps, ParticipantsReadyProps } from '../slots';

/** The column headers: small, bold, uppercase, in the primary colour. */
const headClass = 'h-8 text-[0.78em] font-bold tracking-wide text-primary uppercase';

/** The name cell may wrap (a trial note), the others scroll with the table. */
const nameCellClass = 'min-w-36 whitespace-normal';

const linkClass = 'text-primary underline-offset-4 hover:underline';

/**
 * Werkstatt's participants section: the counts, the bookings as a dense table
 * (present, name, contacts, pass, trial lesson, actions), the add button below
 * it, the canceled bookings and the waiting list, and the dialogs; or the
 * error of a first load that failed.
 */
export function Participants(props: ParticipantsProps) {
  const { t } = useTranslation('schedule');
  if (props.state === 'ready') return <ParticipantsTable {...props} />;
  return (
    <section aria-labelledby={props.headingId} className="space-y-3">
      <Heading id={props.headingId} />
      <Alert variant="destructive">
        <CircleAlert aria-hidden="true" />
        <AlertTitle>{t('participants.loadError')}</AlertTitle>
        <AlertDescription>
          <Button
            variant="outline"
            size="sm"
            className="mt-2 rounded-[3px]"
            onClick={props.onRetry}
          >
            <RotateCcw aria-hidden="true" />
            {t('participants.retry')}
          </Button>
        </AlertDescription>
      </Alert>
    </section>
  );
}

function Heading({ id }: { id: string }) {
  const { t } = useTranslation('schedule');
  return (
    <h3 id={id} className="text-xl font-light">
      {t('participants.title')}
    </h3>
  );
}

function ParticipantsTable({
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
  const actions = canEdit || office;

  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <Heading id={headingId} />
      <p className="text-sm text-muted-foreground">{counts.join(' · ')}</p>
      {stale && (
        <p role="status" className="flex flex-wrap items-center gap-2 text-sm">
          <CircleAlert className="size-4 shrink-0 text-destructive" aria-hidden="true" />
          {t('participants.stale')}
          <Button variant="outline" size="xs" className="rounded-[3px]" onClick={onRetry}>
            <RotateCcw aria-hidden="true" />
            {t('participants.retry')}
          </Button>
        </p>
      )}
      {!canEdit && <p className="text-sm font-medium">{t('participants.dayOver')}</p>}

      {active.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('participants.empty')}</p>
      ) : (
        <Table aria-label={t('participants.listLabel')}>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <TableHead className={headClass}>
                {canAttend ? t('participants.table.present') : t('participants.table.status')}
              </TableHead>
              <TableHead className={headClass}>{t('participants.table.name')}</TableHead>
              <TableHead className={headClass}>{t('participants.table.email')}</TableHead>
              <TableHead className={headClass}>{t('participants.table.phone')}</TableHead>
              <TableHead className={headClass}>{t('participants.table.pass')}</TableHead>
              <TableHead className={headClass}>{t('participants.trialLabel')}</TableHead>
              {actions && (
                <TableHead className={headClass}>
                  <span className="sr-only">{t('participants.table.actions')}</span>
                </TableHead>
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {active.map((p) => {
              const name = shownName(p);
              return (
                <TableRow key={p.bookingId}>
                  <TableCell>
                    {canAttend ? (
                      <Attendance
                        participant={p}
                        name={name}
                        marking={marking(p.bookingId)}
                        mark={(status) => mark(p, status)}
                      />
                    ) : (
                      <StatusText status={p.status} />
                    )}
                  </TableCell>
                  <NameCell participant={p} name={name} />
                  <ContactCells participant={p} />
                  <TableCell>
                    <PaymentBadge payment={p.payment} />
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      {p.trial && (
                        <Badge variant="secondary">{t('participants.trial.badge')}</Badge>
                      )}
                      {canEdit && (
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          className="rounded-[3px]"
                          aria-label={t('participants.trial.editFor', { name })}
                          onClick={() => open(p, 'trial')}
                        >
                          <FlaskConical aria-hidden="true" />
                        </Button>
                      )}
                    </div>
                  </TableCell>
                  {actions && (
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        {office && <HistoryButton name={name} onClick={() => open(p, 'history')} />}
                        {canEdit && (
                          <Button
                            size="icon-xs"
                            className="rounded-[3px] bg-(--werkstatt-red-dark) text-(--werkstatt-red-foreground) hover:bg-(--werkstatt-red-dark)/90"
                            aria-label={t('participants.remove.for', { name })}
                            onClick={() => open(p, 'remove')}
                          >
                            <UserMinus aria-hidden="true" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  )}
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}

      {(canAdd || allAttended.show) && (
        <div className="flex flex-wrap gap-2">
          {canAdd && (
            <Button
              size="sm"
              className="rounded-[3px] bg-(--werkstatt-mint) text-(--werkstatt-mint-foreground) uppercase hover:bg-(--werkstatt-mint)/90"
              onClick={onAdd}
            >
              <UserPlus aria-hidden="true" />
              {t('participants.add.open')}
            </Button>
          )}
          {allAttended.show && (
            <Button
              variant="outline"
              size="sm"
              className="rounded-[3px] uppercase"
              disabled={allAttended.pending}
              onClick={allAttended.run}
            >
              <CheckCheck aria-hidden="true" />
              {t('participants.attendance.all')}
            </Button>
          )}
        </div>
      )}

      {canceled.length > 0 && (
        <SubSection title={t('participants.canceledHeading')}>
          <Table aria-label={t('participants.canceledHeading')}>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className={headClass}>{t('participants.table.name')}</TableHead>
                <TableHead className={headClass}>{t('participants.status.canceled')}</TableHead>
                <TableHead className={headClass}>{t('participants.table.email')}</TableHead>
                <TableHead className={headClass}>{t('participants.table.phone')}</TableHead>
                <TableHead className={headClass}>{t('participants.table.pass')}</TableHead>
                {office && (
                  <TableHead className={headClass}>
                    <span className="sr-only">{t('participants.table.actions')}</span>
                  </TableHead>
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {canceled.map((p) => {
                const name = shownName(p);
                return (
                  <TableRow key={p.bookingId}>
                    <NameCell participant={p} name={name} trialBadge />
                    <TableCell className="text-muted-foreground">
                      {p.canceledAt === undefined
                        ? null
                        : formatDateTime(p.canceledAt, timeZone, lang)}
                    </TableCell>
                    <ContactCells participant={p} />
                    <TableCell>
                      <PaymentBadge payment={p.payment} />
                    </TableCell>
                    {office && (
                      <TableCell>
                        <div className="flex justify-end">
                          <HistoryButton name={name} onClick={() => open(p, 'history')} />
                        </div>
                      </TableCell>
                    )}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </SubSection>
      )}

      {waitlist.length > 0 && (
        <SubSection title={t('participants.waitlist.title')}>
          <ul className="space-y-1.5" aria-label={t('participants.waitlist.title')}>
            {waitlist.map((w) => (
              <WaitlistRow key={w.entryId} entry={w} timeZone={timeZone} />
            ))}
          </ul>
        </SubSection>
      )}

      {dialogs}
    </section>
  );
}

function SubSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="space-y-1 pt-2">
      <h4 className="text-[0.78em] font-bold tracking-wide text-primary uppercase">{title}</h4>
      {children}
    </div>
  );
}

/** A booking's status in words, where attendance cannot be taken now. */
function StatusText({ status }: { status: LessonParticipant['status'] }) {
  const { t } = useTranslation('schedule');
  return (
    <span className={cn(status === 'no_show' && 'text-destructive')}>
      {t(`participants.status.${status}`)}
    </span>
  );
}

/**
 * The present checkbox (checking marks attended, unchecking resets), the
 * no-show button, and the reset of a recorded attendance.
 */
function Attendance({
  participant: p,
  name,
  marking,
  mark,
}: {
  participant: LessonParticipant;
  name: string;
  marking: boolean;
  mark: (status: AttendanceStatus) => void;
}) {
  const { t } = useTranslation('schedule');
  const noShow = p.status === 'no_show';
  return (
    <div
      role="group"
      aria-label={t('participants.attendance.label', { name })}
      className="flex items-center gap-1.5"
    >
      <Checkbox
        className="mx-1"
        checked={p.status === 'attended'}
        disabled={marking}
        aria-label={t('participants.attendance.attendedFor', { name })}
        onCheckedChange={(checked) => mark(checked === true ? 'attended' : 'booked')}
      />
      <Button
        variant={noShow ? 'destructive' : 'ghost'}
        size="icon-xs"
        className="rounded-[3px]"
        aria-pressed={noShow}
        aria-label={t('participants.attendance.noShowFor', { name })}
        disabled={marking}
        onClick={() => !noShow && mark('no_show')}
      >
        <UserX aria-hidden="true" />
      </Button>
      {noShow && (
        <span className="text-xs text-destructive">{t('participants.status.no_show')}</span>
      )}
      {p.status !== 'booked' && (
        <Button
          variant="ghost"
          size="icon-xs"
          className="rounded-[3px]"
          aria-label={t('participants.attendance.resetFor', { name })}
          disabled={marking}
          onClick={() => mark('booked')}
        >
          <RotateCcw aria-hidden="true" />
        </Button>
      )}
    </div>
  );
}

/** The name with the guest and late markers and, of a trial, its note. */
function NameCell({
  participant: p,
  name,
  trialBadge = false,
}: {
  participant: LessonParticipant;
  name: string;
  /** Whether the trial marker goes here (the canceled table has no trial column). */
  trialBadge?: boolean;
}) {
  const { t } = useTranslation('schedule');
  return (
    <TableCell className={nameCellClass}>
      <div className="flex flex-wrap items-center gap-1">
        <span className="min-w-0 font-medium break-words">{name}</span>
        {p.guest && <Badge variant="secondary">{t('participants.guest')}</Badge>}
        {trialBadge && p.trial && (
          <Badge variant="secondary">{t('participants.trial.badge')}</Badge>
        )}
        {p.late && <Badge variant="destructive">{t('participants.late')}</Badge>}
      </div>
      {p.trial && p.trialNote && (
        <p className="text-xs break-words text-muted-foreground">
          {t('participants.trial.note', { note: p.trialNote })}
        </p>
      )}
    </TableCell>
  );
}

/** The contact email and phone as links, each in its own cell. */
function ContactCells({ participant: p }: { participant: LessonParticipant }) {
  return (
    <>
      <TableCell>
        {p.contactEmail && (
          <a href={`mailto:${p.contactEmail}`} className={linkClass}>
            {p.contactEmail}
          </a>
        )}
      </TableCell>
      <TableCell>
        {p.contactPhone && (
          <a href={`tel:${p.contactPhone.replace(/[^\d+]/g, '')}`} className={linkClass}>
            {p.contactPhone}
          </a>
        )}
      </TableCell>
    </>
  );
}

function HistoryButton({ name, onClick }: { name: string; onClick: () => void }) {
  const { t } = useTranslation('schedule');
  return (
    <Button
      variant="ghost"
      size="icon-xs"
      className="rounded-[3px]"
      aria-label={t('participants.history.for', { name })}
      onClick={onClick}
    >
      <History aria-hidden="true" />
    </Button>
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
      <span className={cn('min-w-0 break-words', ended && 'text-muted-foreground')}>
        {shownName(entry)}
      </span>
      <Badge variant={entry.status === 'invited' ? 'default' : 'outline'}>{badge}</Badge>
    </li>
  );
}
