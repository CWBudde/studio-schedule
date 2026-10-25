import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Check, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  acceptWaitlistInviteMutation,
  declineWaitlistInviteMutation,
  leaveWaitlistMutation,
  type WaitlistFor,
} from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { formatDateTime } from '@/lib/schoolTime';
import { useBookingErrorText } from './bookingErrors';
import { inviteRanOut, isInvited } from './waitlist';

/**
 * A person of the family on the waiting list: their place in it and a way
 * to leave it, or the place their invite holds, until when, with Accept
 * (books them) and Decline. An invite whose expiry has passed by the
 * server's clock shows as expired, even before the API says so. `active`
 * is false once the lesson has started or is canceled: then there is
 * nothing to do. `closed`: booking has closed, so no invite can come (nor
 * be taken) any more; the entry may still be left until the lesson starts.
 */
export function WaitlistRow({
  entry,
  self,
  active,
  closed = false,
  timeZone,
  now,
}: {
  entry: WaitlistFor;
  self: boolean;
  active: boolean;
  closed?: boolean;
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
  const invited = !closed && isInvited(entry, now);

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
  if (closed && active && (!expired || entry.stayOnList)) {
    badge = t('booking.waitlist.closed');
    note = t('booking.waitlist.closedHint');
  } else if (invited) {
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
