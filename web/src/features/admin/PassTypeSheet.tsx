import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Save } from 'lucide-react';
import { useMemo } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  createPassTypeMutation,
  type PassType,
  type PassTypeData,
  updatePassTypeMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { TextField } from '@/features/auth/TextField';
import { currencyDigits } from '@/lib/money';
import { SelectField, SwitchField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import {
  passTypeFields,
  passTypeSchema,
  type PassTypeValues,
  passTypeValues,
} from './passTypeForms';

/** What the sheet edits: a new pass type or an existing one. */
export type PassTypeTarget = { mode: 'add' } | { mode: 'edit'; passType: PassType };

/**
 * A sheet with the form of a new or an existing pass type, its amounts in
 * `currency`; closed while `target` is null.
 */
export function PassTypeSheet({
  target,
  currency,
  onClose,
}: {
  target: PassTypeTarget | null;
  currency: string;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <PassTypeForm
          key={shown.mode === 'edit' ? shown.passType.id : 'add'}
          target={shown}
          currency={currency}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function PassTypeForm({
  target,
  currency,
  onDone,
}: {
  target: PassTypeTarget;
  currency: string;
  onDone: () => void;
}) {
  const { t, i18n } = useTranslation('passes');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const lang = i18n.resolvedLanguage ?? 'en';
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createPassTypeMutation(queryClient));
  const update = useMutation(updatePassTypeMutation(queryClient));
  const editing = target.mode === 'edit' ? target.passType : undefined;
  const save = editing ? update : create;
  /** The name of the last attempt, for the message when it is taken. */
  const triedName = (editing ? update.variables?.body : create.variables)?.name ?? '';
  const schema = useMemo(() => passTypeSchema(currencyDigits(currency)), [currency]);
  const form = useForm<PassTypeValues, unknown, PassTypeData>({
    resolver: zodResolver(schema),
    defaultValues: passTypeValues(currency, lang, editing),
  });
  const { errors } = form.formState;
  const kind = useWatch({ control: form.control, name: 'kind' });

  const onSubmit = (body: PassTypeData) => {
    const onError = (error: Error) => showServerErrors(error, form, passTypeFields, 'name');
    const done = (message: 'types.added' | 'types.saved') => () => {
      toast.success(t(message, { name: body.name }));
      onDone();
    };
    if (editing) {
      update.mutate({ passTypeId: editing.id, body }, { onSuccess: done('types.saved'), onError });
    } else {
      create.mutate(body, { onSuccess: done('types.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={editing ? t('types.editTitle', { name: editing.name }) : t('types.addTitle')}
        description={t('types.sheetDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          conflicts={{ duplicate: t('types.duplicate', { name: triedName }) }}
          messages={{ 404: t('types.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('types.name')}
          autoComplete="off"
          error={errorText(errors.name)}
          {...form.register('name')}
        />
        <Controller
          control={form.control}
          name="kind"
          render={({ field }) => (
            <SelectField
              label={t('types.kind')}
              placeholder={t('types.kind')}
              hint={t(`types.kindHints.${field.value}`)}
              options={(['visits', 'time'] as const).map((k) => ({
                value: k,
                label: t(`kinds.${k}`),
              }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.kind)}
            />
          )}
        />
        {kind === 'visits' && (
          <TextField
            label={t('types.visits')}
            type="number"
            inputMode="numeric"
            min={1}
            max={1000}
            error={errorText(errors.visits)}
            {...form.register('visits')}
          />
        )}
        <TextField
          label={kind === 'visits' ? t('types.validityDays') : t('types.durationDays')}
          type="number"
          inputMode="numeric"
          min={kind === 'visits' ? 0 : 1}
          max={3660}
          hint={kind === 'visits' ? t('types.validityHint') : t('types.durationHint')}
          error={errorText(errors.validityDays)}
          {...form.register('validityDays')}
        />
        <TextField
          label={t('types.priceIn', { currency })}
          inputMode="decimal"
          autoComplete="off"
          hint={t('types.priceHint')}
          error={errorText(errors.price)}
          {...form.register('price')}
        />
        <TextField
          label={t('types.vatRatePercent')}
          inputMode="decimal"
          autoComplete="off"
          hint={t('types.vatHint')}
          error={errorText(errors.vatRate)}
          {...form.register('vatRate')}
        />
        <Controller
          control={form.control}
          name="active"
          render={({ field }) => (
            <SwitchField
              label={t('types.active')}
              hint={t('types.activeHint')}
              checked={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.active)}
            />
          )}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? tAdmin('form.save') : t('types.add')}
          icon={editing ? <Save aria-hidden="true" /> : <Plus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
