import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Gift, ShoppingBag } from 'lucide-react';
import { useId, useMemo } from 'react';
import { Controller, useForm, type UseFormReturn, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type AssignPassData,
  assignPassMutation,
  type PassType,
  passTypesQuery,
  type PaymentMethod,
  type PersonDetail,
  type PersonMatch,
  receiptSettingsQuery,
  type SellPassData,
  sellPassMutation,
} from '@/api/queries';
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
import { invalidFields } from '@/features/auth/form';
import { useMoney } from '@/features/passes/useMoney';
import { conflictReason } from '@/api/conflict';
import { currencyDigits } from '@/lib/money';
import { SelectField, TextareaField } from './crud/fields';
import { LoadError } from './crud/LoadError';
import { useAdminErrorText } from './crud/form';
import { personName } from '@/lib/personName';
import { PersonField } from './crud/PersonField';
import {
  assignPassFields,
  assignPassSchema,
  defaultPayer,
  maxPassNote,
  type PassSaleValues,
  passSaleValues,
  sellPassFields,
  sellPassSchema,
} from './passForms';
import { moneyValue } from './passTypeForms';
import { ReceiptSettingsNotice } from './ReceiptSettingsNotice';

const paymentMethods = ['cash', 'transfer', 'card'] as const satisfies PaymentMethod[];

/** Whether the office sells a pass or gives one away. */
export type PassSaleMode = 'sell' | 'assign';

/**
 * Sells a pass of an active type to the person (who paid, from when, at
 * which price, with a note) or gives one away (from when, with a note why).
 * A fresh dialog per opening (key); `open` false animates it out.
 */
export function PassSaleDialog({
  mode,
  open,
  person,
  onClose,
}: {
  mode: PassSaleMode;
  open: boolean;
  person: PersonDetail;
  onClose: () => void;
}) {
  const { t } = useTranslation('passes');
  const money = useMoney();
  const { currency } = money;
  // Asked for once the dialog opens, not with the person's page.
  const types = useQuery({ ...passTypesQuery, enabled: open });
  const name = personName(person);
  const active = (types.data ?? []).filter((p) => p.active);

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent showCloseButton={false} className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {mode === 'sell' ? t('sale.sellTitle', { name }) : t('sale.assignTitle', { name })}
          </DialogTitle>
          <DialogDescription>
            {mode === 'sell' ? t('sale.sellDescription') : t('sale.assignDescription')}
          </DialogDescription>
        </DialogHeader>
        {(types.isError && !types.data) || money.isError ? (
          <LoadError
            message={t('sale.typesError')}
            onRetry={() => {
              if (types.isError) void types.refetch();
              if (money.isError) money.retry();
            }}
          />
        ) : !types.data || !currency ? (
          <p role="status" className="text-sm text-muted-foreground">
            {t('sale.loading')}
          </p>
        ) : active.length === 0 ? (
          <>
            <p className="text-sm">{t('sale.noTypes')}</p>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={onClose}>
                {t('sale.close')}
              </Button>
            </DialogFooter>
          </>
        ) : mode === 'sell' ? (
          <SellForm person={person} types={active} currency={currency} onClose={onClose} />
        ) : (
          <AssignForm person={person} types={active} onClose={onClose} />
        )}
      </DialogContent>
    </Dialog>
  );
}

/** The type select, the first day and the note, which both forms have. */
function CommonFields<O>({
  form,
  types,
  typeLabel,
}: {
  form: UseFormReturn<PassSaleValues, unknown, O>;
  types: PassType[];
  typeLabel: (type: PassType) => string;
}) {
  const { t } = useTranslation('passes');
  const { t: tAdmin } = useTranslation('admin');
  const errorText = useAdminErrorText();
  const { errors } = form.formState;
  return (
    <>
      <Controller
        control={form.control}
        name="passTypeId"
        render={({ field }) => (
          <SelectField
            label={t('sale.type')}
            placeholder={t('sale.chooseType')}
            options={types.map((p) => ({ value: p.id, label: typeLabel(p) }))}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.passTypeId)}
          />
        )}
      />
      <TextField
        label={tAdmin('form.optional', { label: t('sale.validFrom') })}
        type="date"
        hint={t('sale.validFromHint')}
        error={errorText(errors.validFrom)}
        {...form.register('validFrom')}
      />
      <TextareaField
        label={tAdmin('form.optional', { label: t('sale.note') })}
        rows={2}
        maxLength={maxPassNote}
        error={errorText(errors.note)}
        {...form.register('note')}
      />
    </>
  );
}

type SaleField = (typeof sellPassFields)[number];

/** Marks the fields a refused sale names: a type retired meanwhile, a payer deleted meanwhile. */
function serverErrors(
  error: unknown,
  setError: (field: SaleField, message: string) => void,
  fields: readonly SaleField[],
) {
  for (const field of invalidFields(error, fields)) {
    setError(
      field,
      field === 'passTypeId' ? 'passTypeInactive' : field === 'payerId' ? 'person' : 'invalid',
    );
  }
}

function SellForm({
  person,
  types,
  currency,
  onClose,
}: {
  person: PersonDetail;
  types: PassType[];
  currency: string;
  onClose: () => void;
}) {
  const { t } = useTranslation('passes');
  const { t: tAuth } = useTranslation('auth');
  const { t: tAdmin } = useTranslation('admin');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const sell = useMutation(sellPassMutation(queryClient));
  const formId = useId();
  const { t: tReceipts } = useTranslation('receipts');
  const settings = useQuery(receiptSettingsQuery);
  const payer = defaultPayer(person);
  const name = personName(person);
  const digits = currencyDigits(currency);
  const schema = useMemo(() => sellPassSchema(digits, person.id), [digits, person.id]);
  const form = useForm<PassSaleValues, unknown, SellPassData>({
    resolver: zodResolver(schema),
    defaultValues: passSaleValues(payer),
  });
  const { errors } = form.formState;
  const [typeId, price, address] = useWatch({
    control: form.control,
    name: ['passTypeId', 'price', 'recipientAddress'],
  });
  const chosen = types.find((p) => p.id === typeId);
  const { format: money } = useMoney();
  const gross = (price.trim() ? moneyValue(price, digits) : undefined) ?? chosen?.price;
  // Above this gross amount the receipt should name the address (a hint).
  const addressAbove = settings.data?.addressRequiredAbove;
  const needsAddress =
    addressAbove !== undefined &&
    gross !== undefined &&
    gross > addressAbove &&
    address.trim() === '';
  const incomplete =
    conflictReason(sell.error) === 'receipt-settings-incomplete' ||
    settings.data?.complete === false;

  /** The recipient of the receipt follows the payer: their name and email. */
  const onPick = (picked: PersonMatch | undefined) => {
    const who = picked ?? person;
    form.setValue('recipientName', personName(who));
    form.setValue('recipientEmail', who.email ?? '');
  };

  const onSubmit = (body: SellPassData) => {
    const pass = types.find((p) => p.id === body.passTypeId)?.name ?? '';
    sell.mutate(
      { personId: person.id, body },
      {
        onSuccess: () => {
          toast.success(t('sale.sold', { pass, name }));
          onClose();
        },
        onError: (error) => {
          // The settings changed meanwhile: what they miss now.
          if (conflictReason(error) === 'receipt-settings-incomplete') {
            void queryClient.invalidateQueries({ queryKey: receiptSettingsQuery.queryKey });
          }
          serverErrors(
            error,
            (field, message) => form.setError(field, { type: 'server', message }),
            sellPassFields,
          );
        },
      },
    );
  };

  return (
    <>
      <form id={formId} noValidate className="grid gap-4" onSubmit={form.handleSubmit(onSubmit)}>
        {incomplete && <ReceiptSettingsNotice missing={settings.data?.missing} />}
        {conflictReason(sell.error) !== 'receipt-settings-incomplete' && (
          <ProblemAlert
            error={sell.error}
            messages={{ 404: t('sale.personGone'), 422: tAuth('form.checkEntries') }}
          />
        )}
        <CommonFields
          form={form}
          types={types}
          typeLabel={(p) => t('sale.typeOption', { name: p.name, price: money(p.price) })}
        />
        <Controller
          control={form.control}
          name="payerId"
          render={({ field }) => (
            <PersonField
              label={t('sale.payer')}
              hint={t('sale.payerHint', { name })}
              value={field.value}
              person={payer}
              onChange={field.onChange}
              onPick={onPick}
              onBlur={field.onBlur}
              error={errorText(errors.payerId)}
            />
          )}
        />
        <TextField
          label={t('sale.priceIn', { currency })}
          inputMode="decimal"
          autoComplete="off"
          hint={
            chosen ? t('sale.priceHint', { price: money(chosen.price) }) : t('sale.priceHintNone')
          }
          error={errorText(errors.price)}
          {...form.register('price')}
        />
        <Controller
          control={form.control}
          name="paymentMethod"
          render={({ field }) => (
            <SelectField
              label={tReceipts('method.label')}
              placeholder={tReceipts('method.label')}
              hint={t('sale.methodHint')}
              options={paymentMethods.map((m) => ({ value: m, label: tReceipts(`method.${m}`) }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.paymentMethod)}
            />
          )}
        />
        <fieldset className="grid gap-4 rounded-lg border p-3">
          <legend className="px-1 text-sm font-medium">{t('sale.recipient')}</legend>
          <TextField
            label={t('sale.recipientName')}
            autoComplete="off"
            maxLength={200}
            error={errorText(errors.recipientName)}
            {...form.register('recipientName')}
          />
          <TextField
            label={tAdmin('form.optional', { label: t('sale.recipientEmail') })}
            type="email"
            autoComplete="off"
            maxLength={254}
            hint={t('sale.recipientEmailHint')}
            error={errorText(errors.recipientEmail)}
            {...form.register('recipientEmail')}
          />
          <TextareaField
            label={tAdmin('form.optional', { label: t('sale.recipientAddress') })}
            rows={3}
            maxLength={500}
            hint={needsAddress ? undefined : t('sale.recipientAddressHint')}
            error={
              needsAddress
                ? t('sale.addressNeeded', { amount: money(addressAbove ?? 0) })
                : errorText(errors.recipientAddress)
            }
            {...form.register('recipientAddress')}
          />
        </fieldset>
      </form>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={sell.isPending} onClick={onClose}>
          {t('sale.cancel')}
        </Button>
        <Button type="submit" form={formId} disabled={sell.isPending}>
          <ShoppingBag aria-hidden="true" />
          {sell.isPending ? t('sale.selling') : t('sale.sell')}
        </Button>
      </DialogFooter>
    </>
  );
}

function AssignForm({
  person,
  types,
  onClose,
}: {
  person: PersonDetail;
  types: PassType[];
  onClose: () => void;
}) {
  const { t } = useTranslation('passes');
  const { t: tAuth } = useTranslation('auth');
  const queryClient = useQueryClient();
  const assign = useMutation(assignPassMutation(queryClient));
  const formId = useId();
  const name = personName(person);
  const form = useForm<PassSaleValues, unknown, AssignPassData>({
    resolver: zodResolver(assignPassSchema),
    defaultValues: passSaleValues(),
  });

  const onSubmit = (body: AssignPassData) => {
    const pass = types.find((p) => p.id === body.passTypeId)?.name ?? '';
    assign.mutate(
      { personId: person.id, body },
      {
        onSuccess: () => {
          toast.success(t('sale.assigned', { pass, name }));
          onClose();
        },
        onError: (error) =>
          serverErrors(
            error,
            (field, message) => form.setError(field, { type: 'server', message }),
            assignPassFields,
          ),
      },
    );
  };

  return (
    <>
      <form id={formId} noValidate className="grid gap-4" onSubmit={form.handleSubmit(onSubmit)}>
        <ProblemAlert
          error={assign.error}
          messages={{ 404: t('sale.personGone'), 422: tAuth('form.checkEntries') }}
        />
        <CommonFields form={form} types={types} typeLabel={(p) => p.name} />
      </form>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={assign.isPending} onClick={onClose}>
          {t('sale.cancel')}
        </Button>
        <Button type="submit" form={formId} disabled={assign.isPending}>
          <Gift aria-hidden="true" />
          {assign.isPending ? t('sale.assigning') : t('sale.assign')}
        </Button>
      </DialogFooter>
    </>
  );
}
