import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Save, UserPlus } from 'lucide-react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { createStaffMutation, type StaffEntry, updateStaffMutation } from '@/api/queries';
import { invalidFields } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import { SelectField, SwitchField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { PersonField } from './crud/PersonField';
import { ProblemAlert } from './crud/ProblemAlert';
import { staffData, staffFields, staffSchema, type StaffValues, staffValues } from './staffForms';

/** What the sheet edits: a new staff member or an existing one. */
export type StaffTarget = { mode: 'add' } | { mode: 'edit'; staff: StaffEntry };

/** A sheet with the form of a new or an existing staff member; closed while `target` is null. */
export function StaffSheet({
  target,
  onClose,
}: {
  target: StaffTarget | null;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <StaffForm
          key={shown.mode === 'edit' ? shown.staff.id : 'add'}
          target={shown}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function StaffForm({ target, onDone }: { target: StaffTarget; onDone: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createStaffMutation(queryClient));
  const update = useMutation(updateStaffMutation(queryClient));
  const editing = target.mode === 'edit' ? target.staff : undefined;
  const save = editing ? update : create;
  const form = useForm<StaffValues>({
    resolver: zodResolver(staffSchema),
    defaultValues: staffValues(editing),
  });
  const { errors } = form.formState;

  const onSubmit = (values: StaffValues) => {
    const body = staffData(values);
    const onError = (error: Error) => {
      showServerErrors(error, form, staffFields);
      // The only invalid person the field can offer is one deleted meanwhile.
      if (invalidFields(error, ['personId']).length > 0) {
        form.setError('personId', { type: 'server', message: 'person' });
      }
    };
    const done = (message: 'staff.added' | 'staff.saved') => () => {
      toast.success(t(message, { name: values.displayName }));
      onDone();
    };
    if (editing) {
      update.mutate({ staffId: editing.id, body }, { onSuccess: done('staff.saved'), onError });
    } else {
      create.mutate(body, { onSuccess: done('staff.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={editing ? t('staff.editTitle', { name: editing.displayName }) : t('staff.addTitle')}
        description={t('staff.sheetDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          messages={{ 404: t('staff.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('staff.displayName')}
          autoComplete="off"
          hint={t('staff.displayNameHint')}
          error={errorText(errors.displayName)}
          {...form.register('displayName')}
        />
        <Controller
          control={form.control}
          name="kind"
          render={({ field }) => (
            <SelectField
              label={t('staff.kind')}
              placeholder={t('staff.kind')}
              options={(['instructor', 'accompanist'] as const).map((kind) => ({
                value: kind,
                label: t(`staff.kinds.${kind}`),
              }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.kind)}
            />
          )}
        />
        <Controller
          control={form.control}
          name="personId"
          render={({ field }) => (
            <PersonField
              label={t('form.optional', { label: t('staff.person') })}
              hint={t('staff.personHint')}
              value={field.value}
              person={editing?.person}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.personId)}
            />
          )}
        />
        <Controller
          control={form.control}
          name="active"
          render={({ field }) => (
            <SwitchField
              label={t('staff.active')}
              hint={t('staff.activeHint')}
              checked={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.active)}
            />
          )}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('staff.add')}
          icon={editing ? <Save aria-hidden="true" /> : <UserPlus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
