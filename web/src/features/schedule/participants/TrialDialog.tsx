import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CircleAlert } from 'lucide-react';
import { useId } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { type LessonParticipant, refusedAt, updateLessonParticipantMutation } from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { SwitchField, TextareaField } from '@/features/admin/crud/fields';
import { useAdminErrorText } from '@/features/admin/crud/form';
import { shownName } from '@/lib/personName';
import { useParticipantErrorText } from './participantErrors';
import { trialData, trialSchema, type TrialValues } from './participantForms';

/** Marks a booking as a trial lesson (or not) with a note for the instructor. */
export function TrialDialog({
  open,
  participant,
  onClose,
}: {
  open: boolean;
  /** The booking, kept while the dialog closes; a fresh dialog per opening (key). */
  participant: LessonParticipant | null;
  onClose: () => void;
}) {
  const { t } = useTranslation('schedule');
  const errorText = useAdminErrorText();
  const participantError = useParticipantErrorText();
  const queryClient = useQueryClient();
  const update = useMutation(updateLessonParticipantMutation(queryClient));
  const formId = useId();
  const form = useForm<TrialValues>({
    resolver: zodResolver(trialSchema),
    defaultValues: { trial: participant?.trial ?? false, trialNote: participant?.trialNote ?? '' },
  });
  const trial = useWatch({ control: form.control, name: 'trial' });
  const name = participant ? shownName(participant) : '';

  const onSubmit = (values: TrialValues) => {
    if (!participant) return;
    update.mutate(
      { bookingId: participant.bookingId, body: trialData(values) },
      {
        onSuccess: () => {
          toast.success(
            values.trial
              ? t('participants.trial.saved', { name })
              : t('participants.trial.unmarked', { name }),
          );
          onClose();
        },
        onError: (error) => {
          if (refusedAt(error, 'body.trialNote')) {
            form.setError('trialNote', { type: 'server', message: 'invalid' });
          }
        },
      },
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !update.isPending) onClose();
      }}
    >
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t('participants.trial.title', { name })}</DialogTitle>
          <DialogDescription>{t('participants.trial.description')}</DialogDescription>
        </DialogHeader>
        <form id={formId} noValidate className="grid gap-4" onSubmit={form.handleSubmit(onSubmit)}>
          <Controller
            control={form.control}
            name="trial"
            render={({ field }) => (
              <SwitchField
                label={t('participants.trialLabel')}
                checked={field.value}
                onChange={field.onChange}
                onBlur={field.onBlur}
              />
            )}
          />
          {trial && (
            <TextareaField
              label={t('participants.trialNoteLabel')}
              rows={3}
              error={errorText(form.formState.errors.trialNote)}
              {...form.register('trialNote')}
            />
          )}
        </form>
        {update.error && !refusedAt(update.error, 'body.trialNote') && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{participantError(update.error, { name })}</AlertTitle>
          </Alert>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={update.isPending} onClick={onClose}>
            {t('participants.cancel')}
          </Button>
          <Button type="submit" form={formId} disabled={update.isPending}>
            {update.isPending ? t('participants.trial.saving') : t('participants.trial.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
