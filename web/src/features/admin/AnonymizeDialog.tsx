import { useMutation, useQueryClient } from '@tanstack/react-query';
import { UserRoundX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { anonymizePersonMutation, type PersonDetail } from '@/api/queries';
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
import { personName } from '@/lib/personName';

/**
 * Asks before anonymizing a person (ADR 0016) and says why the API refuses:
 * the only guardian of a child, an account that must be kept, or a person
 * anonymized meanwhile. The person stays as "Deleted person" with its
 * history; the page shows it so afterwards.
 */
export function AnonymizeDialog({
  person,
  open,
  onClose,
}: {
  person: PersonDetail;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const anonymize = useMutation(anonymizePersonMutation(queryClient));
  const name = personName(person);
  // Once it succeeds, the page shows the anonymized person without this
  // dialog, which may unmount before mutate()'s own callbacks run; the
  // promise of mutateAsync settles anyway. A failure shows in the dialog.
  const run = async () => {
    try {
      await anonymize.mutateAsync(person.id);
    } catch {
      return;
    }
    toast.success(t('persons.anonymize.done', { name }));
    onClose();
  };

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !anonymize.isPending) {
          anonymize.reset();
          onClose();
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('persons.anonymize.title', { name })}</AlertDialogTitle>
          <AlertDialogDescription>{t('persons.anonymize.description')}</AlertDialogDescription>
        </AlertDialogHeader>
        <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
          <li>{t('persons.anonymize.erases')}</li>
          <li>{t('persons.anonymize.removes')}</li>
          <li>{t('persons.anonymize.keeps')}</li>
        </ul>
        <ProblemAlert
          error={anonymize.error}
          conflicts={{
            'sole-guardian': t('persons.anonymize.soleGuardian', { name }),
            'in-use': t('persons.anonymize.accountKept', { name }),
            anonymized: t('persons.anonymize.already', { name }),
            'own-account': t('persons.anonymize.own'),
            'last-admin': t('persons.anonymize.lastAdmin', { name }),
          }}
          messages={{ 403: t('persons.anonymize.adminOnly', { name }), 404: t('persons.notFound') }}
        />
        <AlertDialogFooter>
          <AlertDialogCancel disabled={anonymize.isPending}>{t('delete.cancel')}</AlertDialogCancel>
          {/* No AlertDialogAction: it would close the dialog before the answer. */}
          <Button variant="destructive" disabled={anonymize.isPending} onClick={() => void run()}>
            <UserRoundX aria-hidden="true" />
            {anonymize.isPending ? t('persons.anonymize.pending') : t('persons.anonymize.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
