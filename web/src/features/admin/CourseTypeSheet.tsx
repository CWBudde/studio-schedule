import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Save } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { type CourseType, createCourseTypeMutation, updateCourseTypeMutation } from '@/api/queries';
import { TextField } from '@/features/auth/TextField';
import {
  courseTypeData,
  courseTypeFields,
  courseTypeSchema,
  type CourseTypeValues,
  courseTypeValues,
} from './courseTypeForms';
import { TextareaField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { ProblemAlert } from '@/components/ProblemAlert';

/** What the sheet edits: a new course type or an existing one. */
export type CourseTypeTarget = { mode: 'add' } | { mode: 'edit'; courseType: CourseType };

/** A sheet with the form of a new or an existing course type; closed while `target` is null. */
export function CourseTypeSheet({
  target,
  onClose,
}: {
  target: CourseTypeTarget | null;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <CourseTypeForm
          key={shown.mode === 'edit' ? shown.courseType.id : 'add'}
          target={shown}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function CourseTypeForm({ target, onDone }: { target: CourseTypeTarget; onDone: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createCourseTypeMutation(queryClient));
  const update = useMutation(updateCourseTypeMutation(queryClient));
  const editing = target.mode === 'edit' ? target.courseType : undefined;
  const save = editing ? update : create;
  /** The name of the last attempt, for the message when it is taken. */
  const triedName = (editing ? update.variables?.body : create.variables)?.name ?? '';
  const form = useForm<CourseTypeValues>({
    resolver: zodResolver(courseTypeSchema),
    defaultValues: courseTypeValues(editing),
  });
  const { errors } = form.formState;

  const onSubmit = (values: CourseTypeValues) => {
    const body = courseTypeData(values);
    const onError = (error: Error) => showServerErrors(error, form, courseTypeFields, 'name');
    const done = (message: 'courseTypes.added' | 'courseTypes.saved') => () => {
      toast.success(t(message, { name: values.name }));
      onDone();
    };
    if (editing) {
      update.mutate(
        { courseTypeId: editing.id, body },
        { onSuccess: done('courseTypes.saved'), onError },
      );
    } else {
      create.mutate(body, { onSuccess: done('courseTypes.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={
          editing ? t('courseTypes.editTitle', { name: editing.name }) : t('courseTypes.addTitle')
        }
        description={t('courseTypes.sheetDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          conflicts={{ duplicate: t('courseTypes.duplicate', { name: triedName }) }}
          messages={{ 404: t('courseTypes.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('courseTypes.name')}
          autoComplete="off"
          error={errorText(errors.name)}
          {...form.register('name')}
        />
        <TextareaField
          label={t('form.optional', { label: t('courseTypes.details') })}
          autoComplete="off"
          rows={4}
          error={errorText(errors.description)}
          {...form.register('description')}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('courseTypes.add')}
          icon={editing ? <Save aria-hidden="true" /> : <Plus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
