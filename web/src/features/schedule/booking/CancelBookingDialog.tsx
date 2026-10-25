import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CircleAlert, TriangleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { type BookedFor, cancelBookingMutation } from '@/api/queries';
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
import { useBookingErrorText } from './bookingErrors';

/** Asks before canceling a booking; a refusal keeps the dialog open and says why. */
export function CancelBookingDialog({
  booking,
  lessonId,
  label,
  late,
  onClose,
}: {
  booking: BookedFor | null;
  lessonId: string;
  label: string;
  /** Whether canceling now counts as late, as far as the client can tell. */
  late: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation('schedule');
  const queryClient = useQueryClient();
  const errorText = useBookingErrorText();
  const cancel = useMutation(cancelBookingMutation(queryClient));
  const name = booking?.name ?? '';

  const onConfirm = () => {
    if (!booking) return;
    cancel.mutate(
      { bookingId: booking.bookingId, lessonId },
      {
        onSuccess: ({ late }) => {
          const title = t('booking.canceled', { name });
          if (late) toast.warning(title, { description: t('booking.late') });
          else toast.success(title);
          onClose();
        },
      },
    );
  };

  return (
    <AlertDialog
      open={booking !== null}
      onOpenChange={(open) => {
        if (!open && !cancel.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('booking.confirm.title')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('booking.confirm.description', { name, lesson: label })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {late && (
          <Alert>
            <TriangleAlert aria-hidden="true" />
            <AlertTitle>{t('booking.confirm.late')}</AlertTitle>
          </Alert>
        )}
        {cancel.error && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{errorText(cancel.error, name)}</AlertTitle>
          </Alert>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={cancel.isPending}>
            {t('booking.confirm.keep')}
          </AlertDialogCancel>
          {/* No AlertDialogAction: it would close the dialog before the answer. */}
          <Button variant="destructive" disabled={cancel.isPending} onClick={onConfirm}>
            {cancel.isPending ? t('booking.confirm.canceling') : t('booking.confirm.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
