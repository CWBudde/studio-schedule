import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { Save, UserPlus } from 'lucide-react';
import { useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type AdminPerson,
  ApiError,
  createPersonMutation,
  updatePersonMutation,
} from '@/api/queries';
import { useSchoolToday } from '@/features/account/personForm';
import { TextField } from '@/features/auth/TextField';
import { TextareaField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { personName } from '@/lib/personName';
import { ProblemAlert } from '@/components/ProblemAlert';
import {
  personFields,
  personRecord,
  personSchema,
  type PersonValues,
  personValues,
} from './personForms';

/** The API's message about `field` of a refused (422) write, if it names the field. */
function fieldMessage(error: unknown, field: string): string | undefined {
  if (!(error instanceof ApiError) || error.status !== 422) return undefined;
  const body = error.body as
    { errors?: { location?: string; message?: string }[] | null } | undefined;
  return body?.errors?.find((e) => e.location === `body.${field}`)?.message;
}

/** What the sheet edits: a new person or an existing one. */
export type PersonTarget = { mode: 'add' } | { mode: 'edit'; person: AdminPerson };

/**
 * A sheet with every field of a new or an existing person; closed while
 * `target` is null. A new person's page opens once they are added.
 */
export function PersonSheet({
  target,
  onClose,
}: {
  target: PersonTarget | null;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <PersonForm
          key={shown.mode === 'edit' ? shown.person.id : 'add'}
          target={shown}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function PersonForm({ target, onDone }: { target: PersonTarget; onDone: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const create = useMutation(createPersonMutation(queryClient));
  const update = useMutation(updatePersonMutation(queryClient));
  const editing = target.mode === 'edit' ? target.person : undefined;
  const save = editing ? update : create;
  const today = useSchoolToday();
  const schema = useMemo(() => personSchema(today), [today]);
  const form = useForm<PersonValues>({
    resolver: zodResolver(schema),
    defaultValues: personValues(editing),
  });
  const { errors } = form.formState;

  const onSubmit = (values: PersonValues) => {
    const body = personRecord(values);
    const name = personName(body);
    const onError = (error: Error) => {
      // Every refused field gets the general error; a birth date in the
      // future (the school's today may differ from the browser's) says so.
      showServerErrors(error, form, personFields);
      if (/future/i.test(fieldMessage(error, 'birthDate') ?? '')) {
        form.setError('birthDate', { type: 'server', message: 'birthDateFuture' });
      }
    };
    if (editing) {
      update.mutate(
        { personId: editing.id, body },
        {
          onSuccess: () => {
            toast.success(t('persons.saved', { name }));
            onDone();
          },
          onError,
        },
      );
    } else {
      create.mutate(body, {
        onSuccess: (added) => {
          toast.success(t('persons.added', { name }));
          onDone();
          void navigate({ to: '/admin/persons/$personId', params: { personId: added.id } });
        },
        onError,
      });
    }
  };

  const optional = (label: string) => t('form.optional', { label });

  return (
    <>
      <FormSheetHeader
        title={
          editing ? t('persons.editTitle', { name: personName(editing) }) : t('persons.addTitle')
        }
        description={editing ? t('persons.editDescription') : t('persons.addDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          messages={{ 404: t('persons.notFound'), 422: tAuth('form.checkEntries') }}
          conflicts={{ duplicate: t('persons.memberNoTaken') }}
        />
        <TextField
          label={t('persons.fields.givenName')}
          autoComplete="off"
          error={errorText(errors.givenName)}
          {...form.register('givenName')}
        />
        <TextField
          label={t('persons.fields.familyName')}
          autoComplete="off"
          error={errorText(errors.familyName)}
          {...form.register('familyName')}
        />
        <TextField
          label={optional(t('persons.fields.email'))}
          type="email"
          autoComplete="off"
          error={errorText(errors.email)}
          {...form.register('email')}
        />
        <TextField
          label={optional(t('persons.fields.phone'))}
          type="tel"
          autoComplete="off"
          error={errorText(errors.phone)}
          {...form.register('phone')}
        />
        <TextField
          label={optional(t('persons.fields.birthDate'))}
          type="date"
          max={today}
          autoComplete="off"
          error={errorText(errors.birthDate)}
          {...form.register('birthDate')}
        />
        <TextField
          label={optional(t('persons.fields.memberNo'))}
          autoComplete="off"
          error={errorText(errors.memberNo)}
          {...form.register('memberNo')}
        />
        <TextareaField
          label={optional(t('persons.fields.notes'))}
          hint={t('persons.fields.notesHint')}
          rows={4}
          error={errorText(errors.notes)}
          {...form.register('notes')}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('persons.add')}
          icon={editing ? <Save aria-hidden="true" /> : <UserPlus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
