import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { type MyEvent, eventRegistrationMutation } from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
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
import { RegistrationProblem } from './RegistrationProblem';

export function CancelRegistrationDialog({ item, onClose }: { item: MyEvent; onClose(): void }) {
  const { t } = useTranslation('events');
  const client = useQueryClient();
  const cancel = useMutation(eventRegistrationMutation(client));
  const { registration, event } = item;

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !cancel.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('cancelTitle')}</AlertDialogTitle>
          <AlertDialogDescription>
            {t('cancelQuestion', { person: registration.person.name, event: event.title })}
            {registration.receiptId && <> {t('willRefund')}</>}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {cancel.error && <RegistrationProblem error={cancel.error} />}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={cancel.isPending}>{t('keepRegistration')}</AlertDialogCancel>
          <Button
            requiresOnline
            variant="destructive"
            disabled={cancel.isPending}
            onClick={() =>
              cancel.mutate(
                { registrationId: registration.id, action: 'cancel' },
                { onSuccess: onClose },
              )
            }
          >
            <PendingIcon pending={cancel.isPending} />
            {t('confirmCancellation')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
