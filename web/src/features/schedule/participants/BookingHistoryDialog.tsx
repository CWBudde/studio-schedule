import { useQuery } from '@tanstack/react-query';
import { CircleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { bookingEventsQuery, type LessonParticipant } from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { formatTimestamp } from '@/lib/schoolTime';

/**
 * The history of a booking (office): every change, oldest first, with the
 * new status, when, who (or "automatically") and the late and override
 * flags.
 */
export function BookingHistoryDialog({
  open,
  participant,
  timeZone,
  onClose,
}: {
  open: boolean;
  /** The booking, kept while the dialog closes. */
  participant: LessonParticipant | null;
  timeZone: string;
  onClose: () => void;
}) {
  const { t } = useTranslation('schedule');
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent showCloseButton={false} className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('participants.history.title')}</DialogTitle>
          <DialogDescription>
            {t('participants.history.description', { name: participant?.name ?? '' })}
          </DialogDescription>
        </DialogHeader>
        {participant && <Events bookingId={participant.bookingId} timeZone={timeZone} />}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {t('participants.history.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Events({ bookingId, timeZone }: { bookingId: string; timeZone: string }) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const events = useQuery(bookingEventsQuery(bookingId));

  if (events.isError) {
    return (
      <Alert variant="destructive">
        <CircleAlert aria-hidden="true" />
        <AlertTitle>{t('participants.history.error')}</AlertTitle>
      </Alert>
    );
  }
  if (!events.data) {
    return (
      <div className="space-y-2" role="status" aria-label={t('participants.history.loading')}>
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    );
  }
  if (events.data.length === 0) {
    return <p className="text-muted-foreground">{t('participants.history.empty')}</p>;
  }
  return (
    <ol className="space-y-3" aria-label={t('participants.history.title')}>
      {events.data.map((e) => (
        <li key={e.id} className="space-y-1 border-l-2 pl-3">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="font-medium">{t(`participants.status.${e.status}`)}</span>
            {e.late && <Badge variant="destructive">{t('participants.late')}</Badge>}
            {e.override && <Badge variant="secondary">{t('participants.history.override')}</Badge>}
            {e.note && (
              <Badge variant="outline">
                {e.note === 'waitlist' ? t('participants.history.waitlist') : e.note}
              </Badge>
            )}
          </div>
          <p className="text-xs text-muted-foreground tabular-nums">
            {formatTimestamp(e.at, timeZone, lang)}
          </p>
          <p className="text-xs break-words text-muted-foreground">
            {e.actor
              ? t('participants.history.by', { name: e.actor.name, email: e.actor.email })
              : t('participants.history.system')}
          </p>
        </li>
      ))}
    </ol>
  );
}
