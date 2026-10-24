import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useId } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { maxNoticeMessage, notifyParticipantsMutation, refusedAt } from '@/api/queries';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { FormError } from '@/features/auth/FormError';
import { cn } from '@/lib/utils';
import { messageLength, normalizeMessage, notifySchema, type NotifyValues } from './notifyForm';

/** The lesson a message is about: its ID and its label in words. */
export type NotifyTarget = { lessonId: string; label: string };

/**
 * Writes a message to the participants of a lesson: plain text of at most
 * 1000 characters (counted in code points, as the API does), mailed to the
 * participants with an active booking (their recipient chains, guests with
 * an address) with the sender's address to reply to.
 */
export function NotifyDialog({
  open,
  target,
  onClose,
}: {
  open: boolean;
  /** The lesson, kept while the dialog closes; a fresh dialog per opening (key). */
  target: NotifyTarget | null;
  onClose: () => void;
}) {
  const { t } = useTranslation('teach');
  const queryClient = useQueryClient();
  const notify = useMutation(notifyParticipantsMutation(queryClient));
  const formId = useId();
  const fieldId = useId();
  const counterId = useId();
  const errorId = useId();
  const form = useForm<NotifyValues>({
    resolver: zodResolver(notifySchema),
    defaultValues: { message: '' },
  });
  const message = useWatch({ control: form.control, name: 'message' });
  const length = messageLength(message);
  const fieldError = form.formState.errors.message?.message;

  const onSubmit = (values: NotifyValues) => {
    if (!target) return;
    notify.mutate(
      { lessonId: target.lessonId, message: normalizeMessage(values.message) },
      {
        onSuccess: (recipients) => {
          if (recipients > 0) toast.success(t('notify.sent', { count: recipients }));
          else toast.warning(t('notify.nobody'));
          onClose();
        },
        onError: (error) => {
          if (refusedAt(error, 'body.message')) {
            form.setError('message', { type: 'server', message: 'invalid' });
          }
        },
      },
    );
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !notify.isPending) onClose();
      }}
    >
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{t('notify.title')}</DialogTitle>
          <DialogDescription>
            {t('notify.description', { lesson: target?.label ?? '' })}
          </DialogDescription>
        </DialogHeader>
        <form
          id={formId}
          noValidate
          className="grid gap-1.5"
          onSubmit={form.handleSubmit(onSubmit)}
        >
          <Label htmlFor={fieldId}>{t('notify.message')}</Label>
          <Textarea
            id={fieldId}
            rows={6}
            aria-invalid={fieldError ? true : undefined}
            aria-describedby={fieldError ? `${counterId} ${errorId}` : counterId}
            className="max-h-[40svh]"
            {...form.register('message')}
          />
          <p
            id={counterId}
            className={cn(
              'text-right text-xs text-muted-foreground tabular-nums',
              length > maxNoticeMessage && 'font-medium text-destructive',
            )}
          >
            <span aria-hidden="true">
              {length}/{maxNoticeMessage}
            </span>
            <span className="sr-only">
              {t('notify.counter', { count: length, max: maxNoticeMessage })}
            </span>
          </p>
          {fieldError && (
            <p id={errorId} className="text-xs text-destructive">
              {t(
                `notify.errors.${fieldError === 'required' || fieldError === 'tooLong' ? fieldError : 'invalid'}`,
                {
                  max: maxNoticeMessage,
                },
              )}
            </p>
          )}
        </form>
        {notify.error && !refusedAt(notify.error, 'body.message') && (
          <FormError
            error={notify.error}
            messages={{ 404: t('notify.errors.notFound'), 429: t('notify.errors.tooMany') }}
          />
        )}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={notify.isPending} onClick={onClose}>
            {t('notify.cancel')}
          </Button>
          <Button type="submit" form={formId} disabled={notify.isPending}>
            {notify.isPending ? t('notify.sending') : t('notify.send')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
