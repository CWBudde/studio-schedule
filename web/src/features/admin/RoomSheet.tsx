import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { DoorOpen, Save } from 'lucide-react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { z } from 'zod';
import { createRoomMutation, type Location, type Room, updateRoomMutation } from '@/api/queries';
import { TextField } from '@/features/auth/TextField';
import { SelectField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { ProblemAlert } from '@/components/ProblemAlert';
import { roomFields, type RoomInput, roomSchema, roomValues } from './locationForms';

/** What the sheet edits: a new room or an existing one. */
export type RoomTarget = { mode: 'add' } | { mode: 'edit'; room: Room };

/**
 * A sheet with the form of a new or an existing room at one of `locations`;
 * closed while `target` is null.
 */
export function RoomSheet({
  target,
  locations,
  onClose,
}: {
  target: RoomTarget | null;
  locations: Location[];
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <RoomForm
          key={shown.mode === 'edit' ? shown.room.id : 'add'}
          target={shown}
          locations={locations}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function RoomForm({
  target,
  locations,
  onDone,
}: {
  target: RoomTarget;
  locations: Location[];
  onDone: () => void;
}) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createRoomMutation(queryClient));
  const update = useMutation(updateRoomMutation(queryClient));
  const editing = target.mode === 'edit' ? target.room : undefined;
  const save = editing ? update : create;
  const form = useForm<RoomInput, unknown, z.output<typeof roomSchema>>({
    resolver: zodResolver(roomSchema),
    // With a single location, a new room can only be there.
    defaultValues: roomValues(editing, locations.length === 1 ? locations[0]?.id : ''),
  });
  const { errors } = form.formState;
  const locationName = (id: string | undefined) => locations.find((l) => l.id === id)?.name ?? '';
  /** The last attempt, for the message when its name is taken. */
  const tried = editing ? update.variables?.body : create.variables;

  const onSubmit = (body: z.output<typeof roomSchema>) => {
    const onError = (error: Error) => showServerErrors(error, form, roomFields, 'name');
    const done = (message: 'rooms.added' | 'rooms.saved') => () => {
      toast.success(t(message, { name: body.name }));
      onDone();
    };
    if (editing) {
      update.mutate({ roomId: editing.id, body }, { onSuccess: done('rooms.saved'), onError });
    } else {
      create.mutate(body, { onSuccess: done('rooms.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={editing ? t('rooms.editTitle', { name: editing.name }) : t('rooms.addTitle')}
        description={t('rooms.sheetDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          conflicts={{
            duplicate: t('rooms.duplicate', {
              name: tried?.name ?? '',
              location: locationName(tried?.locationId),
            }),
          }}
          messages={{ 404: t('rooms.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('rooms.name')}
          autoComplete="off"
          error={errorText(errors.name)}
          {...form.register('name')}
        />
        <Controller
          control={form.control}
          name="locationId"
          render={({ field }) => (
            <SelectField
              label={t('rooms.location')}
              placeholder={t('rooms.chooseLocation')}
              options={locations.map((l) => ({ value: l.id, label: l.name }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.locationId)}
            />
          )}
        />
        <TextField
          label={t('rooms.capacity')}
          type="number"
          inputMode="numeric"
          min={1}
          max={1000}
          step={1}
          hint={t('rooms.capacityHint')}
          error={errorText(errors.capacity)}
          {...form.register('capacity')}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('rooms.add')}
          icon={editing ? <Save aria-hidden="true" /> : <DoorOpen aria-hidden="true" />}
        />
      </form>
    </>
  );
}
