import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { MapPinPlus, Save } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { createLocationMutation, type Location, updateLocationMutation } from '@/api/queries';
import { TextField } from '@/features/auth/TextField';
import { TextareaField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { ProblemAlert } from './crud/ProblemAlert';
import {
  locationData,
  locationFields,
  locationSchema,
  type LocationValues,
  locationValues,
} from './locationForms';

/** What the sheet edits: a new location or an existing one. */
export type LocationTarget = { mode: 'add' } | { mode: 'edit'; location: Location };

/** A sheet with the form of a new or an existing location; closed while `target` is null. */
export function LocationSheet({
  target,
  onClose,
}: {
  target: LocationTarget | null;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <LocationForm
          key={shown.mode === 'edit' ? shown.location.id : 'add'}
          target={shown}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function LocationForm({ target, onDone }: { target: LocationTarget; onDone: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createLocationMutation(queryClient));
  const update = useMutation(updateLocationMutation(queryClient));
  const editing = target.mode === 'edit' ? target.location : undefined;
  const save = editing ? update : create;
  /** The name of the last attempt, for the message when it is taken. */
  const triedName = (editing ? update.variables?.body : create.variables)?.name ?? '';
  const form = useForm<LocationValues>({
    resolver: zodResolver(locationSchema),
    defaultValues: locationValues(editing),
  });
  const { errors } = form.formState;

  const onSubmit = (values: LocationValues) => {
    const body = locationData(values);
    const onError = (error: Error) => showServerErrors(error, form, locationFields, 'name');
    const done = (message: 'locations.added' | 'locations.saved') => () => {
      toast.success(t(message, { name: values.name }));
      onDone();
    };
    if (editing) {
      update.mutate(
        { locationId: editing.id, body },
        { onSuccess: done('locations.saved'), onError },
      );
    } else {
      create.mutate(body, { onSuccess: done('locations.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={editing ? t('locations.editTitle', { name: editing.name }) : t('locations.addTitle')}
        description={t('locations.sheetDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          conflicts={{ duplicate: t('locations.duplicate', { name: triedName }) }}
          messages={{ 404: t('locations.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('locations.name')}
          autoComplete="off"
          error={errorText(errors.name)}
          {...form.register('name')}
        />
        <TextareaField
          label={t('form.optional', { label: t('locations.address') })}
          autoComplete="off"
          rows={2}
          error={errorText(errors.address)}
          {...form.register('address')}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('locations.add')}
          icon={editing ? <Save aria-hidden="true" /> : <MapPinPlus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
