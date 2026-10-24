import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CalendarPlus, Save } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { createTermMutation, updateTermMutation } from '@/api/queries';
import { invalidFields } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import type { Term } from '@/features/timetable/timetable';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { ProblemAlert } from './crud/ProblemAlert';
import { termFields, termSchema, type TermValues, termValues } from './termForms';

/** What the sheet edits: a new term or an existing one. */
export type TermTarget = { mode: 'add' } | { mode: 'edit'; term: Term };

/** A sheet with the form of a new or an existing term; closed while `target` is null. */
export function TermSheet({ target, onClose }: { target: TermTarget | null; onClose: () => void }) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <TermForm
          key={shown.mode === 'edit' ? shown.term.id : 'add'}
          target={shown}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function TermForm({ target, onDone }: { target: TermTarget; onDone: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createTermMutation(queryClient));
  const update = useMutation(updateTermMutation(queryClient));
  const editing = target.mode === 'edit' ? target.term : undefined;
  const save = editing ? update : create;
  const form = useForm<TermValues>({
    resolver: zodResolver(termSchema),
    defaultValues: termValues(editing),
  });
  const { errors } = form.formState;

  const onSubmit = (values: TermValues) => {
    const onError = (error: Error) => {
      showServerErrors(error, form, termFields);
      // A date the API refuses (the client checks their order) leaves out a
      // lesson that has to stay: it has started, has bookings or was edited.
      for (const field of invalidFields(error, ['startsOn', 'endsOn'] as const)) {
        form.setError(field, { type: 'server', message: 'termLessons' });
      }
    };
    const done = (message: 'terms.added' | 'terms.saved') => () => {
      toast.success(t(message, { name: values.name }));
      onDone();
    };
    if (editing) {
      update.mutate(
        { termId: editing.id, body: values },
        { onSuccess: done('terms.saved'), onError },
      );
    } else {
      create.mutate(values, { onSuccess: done('terms.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={editing ? t('terms.editTitle', { name: editing.name }) : t('terms.addTitle')}
        description={editing ? t('terms.editDescription') : t('terms.addDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          messages={{ 404: t('terms.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('terms.name')}
          autoComplete="off"
          hint={t('terms.nameHint')}
          error={errorText(errors.name)}
          {...form.register('name')}
        />
        <TextField
          label={t('terms.startsOn')}
          type="date"
          error={errorText(errors.startsOn)}
          {...form.register('startsOn')}
        />
        <TextField
          label={t('terms.endsOn')}
          type="date"
          hint={t('terms.endsOnHint')}
          error={errorText(errors.endsOn)}
          {...form.register('endsOn')}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('terms.add')}
          icon={editing ? <Save aria-hidden="true" /> : <CalendarPlus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
