import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Save } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { conflictReason } from '@/api/conflict';
import { createLevelMutation, type Level, updateLevelMutation } from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { TextField } from '@/features/auth/TextField';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { levelData, levelFields, levelSchema, type LevelValues, levelValues } from './levelForms';

/** What the sheet edits: a new level or an existing one. */
export type LevelTarget = { mode: 'add' } | { mode: 'edit'; level: Level };

/** A sheet with the form of a new or an existing level; closed while `target` is null. */
export function LevelSheet({
  target,
  onClose,
}: {
  target: LevelTarget | null;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <LevelForm
          key={shown.mode === 'edit' ? shown.level.id : 'add'}
          target={shown}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function LevelForm({ target, onDone }: { target: LevelTarget; onDone: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createLevelMutation(queryClient));
  const update = useMutation(updateLevelMutation(queryClient));
  const editing = target.mode === 'edit' ? target.level : undefined;
  const save = editing ? update : create;
  /** The name and code of the last attempt, for the message when one is taken. */
  const tried = (editing ? update.variables?.body : create.variables) ?? { name: '' };
  const form = useForm<LevelValues>({
    resolver: zodResolver(levelSchema),
    defaultValues: levelValues(editing),
  });
  const { errors } = form.formState;

  const onSubmit = (values: LevelValues) => {
    const body = levelData(values);
    // A 409 `duplicate` does not say whether the name or the code is taken.
    const onError = (error: Error) => {
      showServerErrors(error, form, levelFields);
      if (conflictReason(error) === 'duplicate')
        form.setError('name', { type: 'server', message: 'nameOrCodeTaken' });
    };
    const done = (message: 'levels.added' | 'levels.saved') => () => {
      toast.success(t(message, { name: values.name }));
      onDone();
    };
    if (editing) {
      update.mutate({ levelId: editing.id, body }, { onSuccess: done('levels.saved'), onError });
    } else {
      create.mutate(body, { onSuccess: done('levels.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={editing ? t('levels.editTitle', { name: editing.name }) : t('levels.addTitle')}
        description={t('levels.sheetDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          conflicts={{
            duplicate: tried.code
              ? t('levels.duplicateWithCode', { name: tried.name, code: tried.code })
              : t('levels.duplicate', { name: tried.name }),
          }}
          messages={{ 404: t('levels.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('levels.name')}
          autoComplete="off"
          error={errorText(errors.name)}
          {...form.register('name')}
        />
        <TextField
          label={t('form.optional', { label: t('levels.code') })}
          hint={t('levels.codeHint')}
          autoComplete="off"
          error={errorText(errors.code)}
          {...form.register('code')}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('levels.add')}
          icon={editing ? <Save aria-hidden="true" /> : <Plus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
