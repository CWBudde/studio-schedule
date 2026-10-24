import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleAlert } from 'lucide-react';
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
import { Skeleton } from '@/components/ui/skeleton';
import { formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';
import { useBookingErrorText } from './bookingErrors';

/**
 * Cancels a booking of a class lesson together with the same person's later
 * bookings of that class. It shows what the API would cancel (dates in
 * school time, late ones marked) and cancels on confirmation; the toast says
 * how many, and how many of them were late.
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
  const preview = useQuery({
    ...cancelFollowingPreviewQuery(bookingId),
    enabled: booking !== null,
  });
  const apply = useMutation(cancelFollowingMutation(queryClient));
  const name = booking?.name ?? '';
  const bookings = preview.data?.bookings ?? [];
  const error = apply.error ?? preview.error;

  const onConfirm = () =>
    apply.mutate(bookingId, {
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
    });

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
