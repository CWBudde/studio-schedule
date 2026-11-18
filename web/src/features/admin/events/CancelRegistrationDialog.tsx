import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Ban } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { type EventRegistration, cancelEventRegistrationAsOfficeMutation } from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
import { ProblemAlert } from '@/components/ProblemAlert';
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
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';

export function CancelRegistrationDialog({
  registration,
  name,
  onClose,
}: {
  registration: EventRegistration;
  name: string;
  onClose: () => void;
}) {
  const { t } = useTranslation('adminEvents');
  const { t: tAdmin } = useTranslation('admin');
  const queryClient = useQueryClient();
  const cancel = useMutation(cancelEventRegistrationAsOfficeMutation(queryClient, registration.id));
  const [refund, setRefund] = useState(false);
  const refundId = useId();
  const hintId = useId();
  const canRefund = registration.payment === 'paid' && !!registration.receiptId;

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !cancel.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('registrations.cancel')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('registrations.cancelDescription', { name })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ProblemAlert
          error={cancel.error}
          conflicts={{
            'already-canceled': t('conflicts.already-canceled'),
            'receipt-settings-incomplete': t('conflicts.receipt-settings-incomplete'),
          }}
        />
        {canRefund && (
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Checkbox
                id={refundId}
                checked={refund}
                aria-describedby={hintId}
                disabled={cancel.isPending}
                onCheckedChange={(value) => setRefund(value === true)}
              />
              <Label htmlFor={refundId}>{t('registrations.refund')}</Label>
            </div>
            <p id={hintId} className="text-sm text-muted-foreground">
              {t('registrations.refundHint')}
            </p>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={cancel.isPending}>
            {tAdmin('lessons.back')}
          </AlertDialogCancel>
          <Button
            type="button"
            variant="destructive"
            disabled={cancel.isPending}
            onClick={() =>
              cancel.mutate(canRefund && refund, {
                onSuccess: () => {
                  toast.success(t('registrations.canceled'));
                  onClose();
                },
              })
            }
          >
            <PendingIcon pending={cancel.isPending} icon={<Ban aria-hidden="true" />} />
            {t(cancel.isPending ? 'actions.canceling' : 'registrations.cancel')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
