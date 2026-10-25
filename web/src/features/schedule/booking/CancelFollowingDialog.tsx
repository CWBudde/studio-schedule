import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleAlert } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type BookedFor,
  cancelFollowingMutation,
  cancelFollowingPreviewQuery,
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
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import {
  addDays,
  formatDate,
  formatDayHeading,
  formatTimeRange,
  schoolDateOf,
} from '@/lib/schoolTime';
import { useBookingErrorText } from './bookingErrors';

/**
 * Cancels a booking of a class lesson together with the same person's later
 * bookings of that class up to a date, by default the end of the term (the
 * first preview names it). It shows what the API would cancel (dates in
 * school time, late ones marked), asked again when the date changes, says
 * when a member of the class stays one and is booked again after the date,
 * and cancels on confirmation; the toast says how many, and how many of them
 * were late.
 */
export function CancelFollowingDialog({
  booking,
  timeZone,
  onClose,
}: {
  booking: BookedFor | null;
  timeZone: string;
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const queryClient = useQueryClient();
  const errorText = useBookingErrorText();
  const bookingId = booking?.bookingId ?? '';
  const untilId = useId();
  const hintId = useId();
  // The date chosen; empty for the default, which the preview names.
  const [until, setUntil] = useState('');
  const [shownFor, setShownFor] = useState(bookingId);
  if (shownFor !== bookingId) {
    setShownFor(bookingId);
    setUntil('');
  }
  const preview = useQuery({
    ...cancelFollowingPreviewQuery(bookingId, until || undefined),
    enabled: booking !== null,
  });
  const apply = useMutation(cancelFollowingMutation(queryClient));
  const name = booking?.name ?? '';
  const bookings = preview.data?.bookings ?? [];
  const pausedUntil = preview.data?.pausedUntil;
  const error = apply.error ?? preview.error;

  const onConfirm = () =>
    apply.mutate(
      { bookingId, until: preview.data?.until },
      {
        onSuccess: ({ bookings: canceled }) => {
          const late = canceled.filter((b) => b.late).length;
          const title = t('booking.following.done', { count: canceled.length });
          if (late > 0) {
            toast.warning(title, { description: t('booking.following.doneLate', { count: late }) });
          } else {
            toast.success(title);
          }
          onClose();
        },
      },
    );

  return (
    <AlertDialog
      open={booking !== null}
      onOpenChange={(open) => {
        if (!open && !apply.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('booking.following.title')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('booking.following.description', { name })}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className="space-y-1.5">
          <Label htmlFor={untilId}>{t('booking.following.untilLabel')}</Label>
          <Input
            id={untilId}
            type="date"
            aria-describedby={hintId}
            value={until || (preview.data?.until ?? '')}
            disabled={apply.isPending}
            onChange={(e) => setUntil(e.target.value)}
          />
          <p id={hintId} className="text-xs text-muted-foreground">
            {t('booking.following.untilHint')}
          </p>
        </div>

        {preview.isPending && booking !== null && (
          <div className="space-y-2" aria-busy="true">
            <span className="sr-only">{t('booking.following.loading')}</span>
            <Skeleton className="h-5 w-full" />
            <Skeleton className="h-5 w-3/4" />
          </div>
        )}
        {bookings.length > 0 && (
          <>
            <ul
              aria-label={t('booking.following.title')}
              className="max-h-64 space-y-1 overflow-y-auto"
            >
              {bookings.map((b) => (
                <li key={b.bookingId} className="flex flex-wrap items-center gap-x-2 text-sm">
                  <span className="font-medium">
                    {formatDayHeading(schoolDateOf(b.startsAt, timeZone), lang)}
                  </span>
                  <span className="text-muted-foreground tabular-nums">
                    {formatTimeRange(b.startsAt, b.endsAt, timeZone, lang)}
                  </span>
                  {b.late && <Badge variant="destructive">{t('booking.following.late')}</Badge>}
                </li>
              ))}
            </ul>
            {bookings.some((b) => b.late) && (
              <p className="text-sm text-muted-foreground">{t('booking.following.lateHint')}</p>
            )}
            {pausedUntil && (
              <p className="text-sm">
                {t('booking.following.paused', {
                  name,
                  date: formatDate(addDays(pausedUntil, 1), lang, {
                    weekday: 'long',
                    day: 'numeric',
                    month: 'long',
                    year: 'numeric',
                  }),
                })}
              </p>
            )}
          </>
        )}
        {error && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{errorText(error, name)}</AlertTitle>
          </Alert>
        )}

        <AlertDialogFooter>
          <AlertDialogCancel disabled={apply.isPending}>
            {t('booking.following.keep')}
          </AlertDialogCancel>
          <Button
            requiresOnline
            variant="destructive"
            disabled={apply.isPending || bookings.length === 0}
            onClick={onConfirm}
          >
            {apply.isPending
              ? t('booking.following.canceling')
              : t('booking.following.confirm', { count: bookings.length })}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
