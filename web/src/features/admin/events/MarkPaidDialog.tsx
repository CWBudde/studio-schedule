import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useId } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { conflictReason } from '@/api/conflict';
import {
  type EventPaymentData,
  type EventRegistration,
  markEventRegistrationPaidMutation,
  receiptSettingsQuery,
} from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { TextField } from '@/features/auth/TextField';
import { useMoney } from '@/features/passes/useMoney';
import { personName } from '@/lib/personName';
import { ReceiptSettingsNotice } from '../ReceiptSettingsNotice';
import { PersonField } from '../crud/PersonField';
import { SelectField, TextareaField } from '../crud/fields';
import { showServerErrors, useAdminErrorText } from '../crud/form';
import { paymentFields, paymentSchema, type PaymentValues } from './eventForms';

const methods = ['cash', 'transfer', 'card'] as const;

export function MarkPaidDialog({
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
  const { t: tAuth } = useTranslation('auth');
  const { t: tPasses } = useTranslation('passes');
  const { t: tReceipts } = useTranslation('receipts');
  const errorText = useAdminErrorText();
  const { format: money } = useMoney();
  const queryClient = useQueryClient();
  const settings = useQuery(receiptSettingsQuery);
  const pay = useMutation(markEventRegistrationPaidMutation(queryClient, registration.id));
  const formId = useId();
  const form = useForm<PaymentValues, unknown, EventPaymentData>({
    resolver: zodResolver(paymentSchema),
    defaultValues: {
      payerId: '',
      paymentMethod: 'cash',
      recipientName: name,
      recipientAddress: '',
      recipientEmail: registration.guest?.email ?? '',
    },
  });
  const { errors } = form.formState;
  const address = useWatch({ control: form.control, name: 'recipientAddress' });
  const threshold = settings.data?.addressRequiredAbove;
  const needsAddress = threshold !== undefined && registration.price > threshold && !address.trim();
  const incomplete = settings.data?.complete === false;
  const onSubmit = (body: EventPaymentData) =>
    pay.mutate(body, {
      onSuccess: ({ receipt }) => {
        toast.success(t('registrations.markedPaid', { number: receipt.number }));
        onClose();
      },
      onError: (error) => {
        showServerErrors(error, form, paymentFields);
        if (conflictReason(error) === 'receipt-settings-incomplete') void settings.refetch();
      },
    });

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pay.isPending) onClose();
      }}
    >
      <DialogContent showCloseButton={false} className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('registrations.markPaid')}</DialogTitle>
          <DialogDescription>
            {t('registrations.paymentDescription', { name, amount: money(registration.price) })}
          </DialogDescription>
        </DialogHeader>
        {incomplete && <ReceiptSettingsNotice missing={settings.data?.missing} />}
        <ProblemAlert
          error={pay.error}
          conflicts={{
            'already-paid': t('conflicts.already-paid'),
            'already-canceled': t('conflicts.already-canceled'),
            'not-registered': t('conflicts.not-registered'),
            'receipt-settings-incomplete': t('conflicts.receipt-settings-incomplete'),
          }}
          messages={{ 422: tAuth('form.checkEntries') }}
        />
        <form id={formId} noValidate className="grid gap-4" onSubmit={form.handleSubmit(onSubmit)}>
          {registration.person && (
            <Controller
              control={form.control}
              name="payerId"
              render={({ field }) => (
                <PersonField
                  label={tPasses('sale.payer')}
                  hint={tPasses('sale.payerHint', { name })}
                  value={field.value}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                  onPick={(payer) => {
                    form.setValue('recipientName', payer ? personName(payer) : name);
                    form.setValue('recipientEmail', payer?.email ?? '');
                  }}
                  error={errorText(errors.payerId)}
                />
              )}
            />
          )}
          <Controller
            control={form.control}
            name="paymentMethod"
            render={({ field }) => (
              <SelectField
                label={tReceipts('method.label')}
                placeholder={tReceipts('method.label')}
                options={methods.map((method) => ({
                  value: method,
                  label: tReceipts(`method.${method}`),
                }))}
                value={field.value}
                onChange={field.onChange}
                onBlur={field.onBlur}
                error={errorText(errors.paymentMethod)}
              />
            )}
          />
          <fieldset className="grid gap-4 rounded-lg border p-3">
            <legend className="px-1 text-sm font-medium">{tPasses('sale.recipient')}</legend>
            <TextField
              label={tPasses('sale.recipientName')}
              autoComplete="off"
              maxLength={200}
              error={errorText(errors.recipientName)}
              {...form.register('recipientName')}
            />
            <TextField
              label={tAdmin('form.optional', { label: tPasses('sale.recipientEmail') })}
              type="email"
              autoComplete="off"
              maxLength={254}
              hint={tPasses('sale.recipientEmailHint')}
              error={errorText(errors.recipientEmail)}
              {...form.register('recipientEmail')}
            />
            <TextareaField
              label={tAdmin('form.optional', { label: tPasses('sale.recipientAddress') })}
              rows={3}
              maxLength={500}
              hint={tPasses('sale.recipientAddressHint')}
              error={
                needsAddress
                  ? tPasses('sale.addressNeeded', { amount: money(threshold) })
                  : errorText(errors.recipientAddress)
              }
              {...form.register('recipientAddress')}
            />
          </fieldset>
        </form>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={pay.isPending} onClick={onClose}>
            {tAdmin('form.cancel')}
          </Button>
          <Button type="submit" form={formId} disabled={pay.isPending || incomplete}>
            <PendingIcon pending={pay.isPending} />
            {t(pay.isPending ? 'registrations.paying' : 'registrations.markPaid')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
