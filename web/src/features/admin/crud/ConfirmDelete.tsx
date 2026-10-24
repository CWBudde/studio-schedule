import { Trash2 } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
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
import type { ConflictReason } from '@/api/conflict';
import { ProblemAlert } from '@/components/ProblemAlert';

/**
 * Asks before deleting a record. A refused delete keeps the dialog open and
 * says why (the sentence of `conflicts` for the 409 reason); the caller
 * closes it when the delete succeeds. `alternative` is a button the caller
 * offers next to the delete, such as deactivating instead.
 */
export function ConfirmDelete({
  open,
  title,
  description,
  pending,
  deleting = pending,
  error,
  conflicts,
  messages,
  alternative,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  description: string;
  /** Whether the delete, or the alternative, is under way. */
  pending: boolean;
  /** Whether the delete itself is under way; `pending` if not given. */
  deleting?: boolean;
  /** The error of the last attempt, if it failed. */
  error: unknown;
  conflicts?: Partial<Record<ConflictReason, string>>;
  messages?: Partial<Record<number, string>>;
  alternative?: ReactNode;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation('admin');
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onCancel();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <ProblemAlert error={error} conflicts={conflicts} messages={messages} />
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t('delete.cancel')}</AlertDialogCancel>
          {alternative}
          {/* No AlertDialogAction: it would close the dialog before the answer. */}
          <Button variant="destructive" disabled={pending} onClick={onConfirm}>
            <Trash2 aria-hidden="true" />
            {deleting ? t('delete.deleting') : t('delete.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
