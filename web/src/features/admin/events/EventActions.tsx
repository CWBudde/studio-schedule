import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Ban, Eye, EyeOff, Pencil, Trash2 } from 'lucide-react';
import { useId, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type AdminEvent,
  cancelEventMutation,
  deleteEventMutation,
  publishEventMutation,
} from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { cancelSchema, type CancelValues } from '@/features/schedule/admin/lessonForms';
import { DeleteDialog } from '../crud/DeleteDialog';
import { TextareaField } from '../crud/fields';
import { showServerErrors, useAdminErrorText } from '../crud/form';
import { EventSheet } from './EventSheet';

type Action = 'edit' | 'cancel' | 'delete';

export function EventActions({ event, onDeleted }: { event: AdminEvent; onDeleted?: () => void }) {
  const { t } = useTranslation('adminEvents');
  const queryClient = useQueryClient();
  const [action, setAction] = useState<Action>();
  const isDraft = event.status === 'draft';
  const changeStatus = useMutation(publishEventMutation(queryClient, isDraft));
  const close = () => setAction(undefined);

  return (
    <div className="space-y-2">
      <ProblemAlert
        error={changeStatus.error}
        conflicts={{
          'no-sessions': t('conflicts.no-sessions'),
          'not-draft': t('conflicts.not-draft'),
          'not-published': t('conflicts.not-published'),
          'in-use': t('actions.unpublishInUse'),
        }}
        messages={{ 404: t('notFound') }}
      />
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={changeStatus.isPending}
          onClick={() => setAction('edit')}
        >
          <Pencil aria-hidden="true" />
          {t('edit')}
        </Button>
        {event.status !== 'canceled' && (
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={changeStatus.isPending}
              onClick={() => changeStatus.mutate(event.id)}
            >
              <PendingIcon
                pending={changeStatus.isPending}
                icon={isDraft ? <Eye aria-hidden="true" /> : <EyeOff aria-hidden="true" />}
              />
              {t(isDraft ? 'actions.publish' : 'actions.unpublish')}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={changeStatus.isPending}
              onClick={() => setAction('cancel')}
            >
              <Ban aria-hidden="true" />
              {t('actions.cancel')}
            </Button>
          </>
        )}
        <Button
          size="sm"
          variant="outline"
          disabled={changeStatus.isPending}
          onClick={() => setAction('delete')}
        >
          <Trash2 aria-hidden="true" />
          {t('actions.delete')}
        </Button>
      </div>
      <EventSheet target={action === 'edit' ? { mode: 'edit', event } : null} onClose={close} />
      {action === 'cancel' && <CancelEventDialog event={event} onClose={close} />}
      <DeleteDialog
        target={action === 'delete' ? event : null}
        id={(record) => record.id}
        mutation={deleteEventMutation}
        texts={({ title }) => ({
          title: t('actions.deleteTitle', { title }),
          description: t('actions.deleteDescription'),
          deleted: t('actions.deleted'),
          inUse: t('actions.deleteInUse'),
          notFound: t('notFound'),
        })}
        onDeleted={onDeleted}
        onClose={close}
      />
    </div>
  );
}

function CancelEventDialog({ event, onClose }: { event: AdminEvent; onClose: () => void }) {
  const { t } = useTranslation('adminEvents');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const cancel = useMutation(cancelEventMutation(queryClient));
  const formId = useId();
  const form = useForm<CancelValues>({
    resolver: zodResolver(cancelSchema),
    defaultValues: { note: '' },
  });
  const onSubmit = ({ note }: CancelValues) =>
    cancel.mutate(
      { eventId: event.id, body: note ? { note } : {} },
      {
        onSuccess: () => {
          toast.success(t('actions.canceled'));
          onClose();
        },
        onError: (error) => showServerErrors(error, form, ['note']),
      },
    );

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !cancel.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('actions.cancelTitle', { title: event.title })}</AlertDialogTitle>
          <AlertDialogDescription>{t('actions.cancelDescription')}</AlertDialogDescription>
        </AlertDialogHeader>
        <ProblemAlert
          error={cancel.error}
          conflicts={{
            closed: t('conflicts.closed'),
            'receipt-settings-incomplete': t('conflicts.receipt-settings-incomplete'),
          }}
          messages={{ 404: t('notFound'), 422: tAuth('form.checkEntries') }}
        />
        <form id={formId} noValidate onSubmit={form.handleSubmit(onSubmit)}>
          <TextareaField
            label={tAdmin('lessons.noteLabel')}
            rows={3}
            error={errorText(form.formState.errors.note)}
            {...form.register('note')}
          />
        </form>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={cancel.isPending}>
            {tAdmin('lessons.back')}
          </AlertDialogCancel>
          <Button type="submit" form={formId} variant="destructive" disabled={cancel.isPending}>
            <PendingIcon pending={cancel.isPending} icon={<Ban aria-hidden="true" />} />
            {t(cancel.isPending ? 'actions.canceling' : 'actions.cancel')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
