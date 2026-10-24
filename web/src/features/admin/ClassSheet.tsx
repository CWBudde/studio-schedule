import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CalendarPlus, Save } from 'lucide-react';
import { useMemo } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type ClassEntry,
  type CourseType,
  createClassMutation,
  type Location,
  type Room,
  type StaffEntry,
  updateClassMutation,
} from '@/api/queries';
import type { components } from '@/api/schema';
import { invalidFields } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import { levels } from '@/features/schedule/search';
import { weekdayName } from '@/features/timetable/timetable';
import {
  classData,
  classFields,
  type ClassInput,
  type ClassOutput,
  classSchema,
  classValues,
  weekdaysMondayFirst,
} from './classForms';
import { SelectField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { ProblemAlert } from './crud/ProblemAlert';
import { useClassLabel } from './useClassLabel';

/** What the sheet edits: a new class of the term or an existing one. */
export type ClassTarget = { mode: 'add' } | { mode: 'edit'; entry: ClassEntry };

/** The catalogue the form offers. */
export type ClassChoices = {
  courseTypes: CourseType[];
  rooms: Room[];
  locations: Location[];
  staff: StaffEntry[];
};

/** The select value of "no accompanist": Radix Select has no empty values. */
const none = 'none';

/**
 * A sheet with the form of a new class of the term `termId` or of an
 * existing one; closed while `target` is null.
 */
export function ClassSheet({
  termId,
  target,
  choices,
  onClose,
}: {
  termId: string;
  target: ClassTarget | null;
  choices: ClassChoices;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <ClassForm
          key={shown.mode === 'edit' ? shown.entry.id : 'add'}
          termId={termId}
          target={shown}
          choices={choices}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

/**
 * The options of a staff select: the active staff of `kind` and, so an
 * existing class keeps them, its `current` member even when inactive.
 */
function staffOptions(
  staff: StaffEntry[],
  kind: StaffEntry['kind'],
  current: components['schemas']['StaffRef'] | undefined,
  inactive: (name: string) => string,
) {
  const options = staff
    .filter((s) => s.id === current?.id || (s.kind === kind && s.active))
    .map((s) => ({ value: s.id, label: s.active ? s.displayName : inactive(s.displayName) }));
  if (current && !options.some((o) => o.value === current.id)) {
    options.push({ value: current.id, label: current.displayName });
  }
  return options;
}

/** Which server-refused fields get a message of their own rather than the general one. */
const serverMessages: Partial<Record<keyof ClassInput, string>> = {
  capacity: 'bookedCapacity',
  durationMinutes: 'midnight',
  accompanistId: 'accompanist',
};

function ClassForm({
  termId,
  target,
  choices,
  onDone,
}: {
  termId: string;
  target: ClassTarget;
  choices: ClassChoices;
  onDone: () => void;
}) {
  const { t, i18n } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const { t: tSchedule } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const errorText = useAdminErrorText();
  const classLabel = useClassLabel(choices.courseTypes);
  const queryClient = useQueryClient();
  const create = useMutation(createClassMutation(queryClient));
  const update = useMutation(updateClassMutation(queryClient));
  const editing = target.mode === 'edit' ? target.entry : undefined;
  const save = editing ? update : create;
  const form = useForm<ClassInput, unknown, ClassOutput>({
    resolver: zodResolver(classSchema),
    defaultValues: classValues(editing),
  });
  const { errors } = form.formState;

  const options = useMemo(() => {
    const locationName = new Map(choices.locations.map((l) => [l.id, l.name]));
    const inactive = (name: string) => t('classes.inactiveStaff', { name });
    return {
      courseTypes: choices.courseTypes.map((c) => ({ value: c.id, label: c.name })),
      rooms: choices.rooms
        .map((r) => ({
          value: r.id,
          label: `${locationName.get(r.locationId) ?? t('classes.unknown')} · ${r.name}`,
        }))
        .sort((a, b) => a.label.localeCompare(b.label, lang)),
      instructors: staffOptions(choices.staff, 'instructor', editing?.instructor, inactive),
      accompanists: [
        { value: none, label: t('classes.noAccompanist') },
        ...staffOptions(choices.staff, 'accompanist', editing?.accompanist, inactive),
      ],
      levels: levels.map((l) => ({ value: l, label: tSchedule(`level.${l}`) })),
      weekdays: weekdaysMondayFirst.map((d) => ({
        value: String(d),
        label: weekdayName(d, lang),
      })),
    };
  }, [choices, editing, lang, t, tSchedule]);

  /** A new class gets the capacity of its room, unless the capacity was typed. */
  const pickRoom = (roomId: string) => {
    form.setValue('roomId', roomId, { shouldValidate: form.formState.isSubmitted });
    const room = choices.rooms.find((r) => r.id === roomId);
    if (!editing && room && !form.getFieldState('capacity').isDirty) {
      form.setValue('capacity', String(room.capacity), {
        shouldValidate: form.formState.isSubmitted,
      });
    }
  };

  const onSubmit = (values: ClassOutput) => {
    const body = classData(values, editing?.retired ?? false);
    const name = classLabel(values);
    const onError = (error: Error) => {
      showServerErrors(error, form, classFields);
      for (const field of invalidFields(error, classFields)) {
        const message = serverMessages[field];
        if (message) form.setError(field, { type: 'server', message });
      }
    };
    const done = (message: 'classes.added' | 'classes.saved') => () => {
      toast.success(t(message, { name }));
      onDone();
    };
    if (editing) {
      update.mutate({ classId: editing.id, body }, { onSuccess: done('classes.saved'), onError });
    } else {
      create.mutate({ termId, body }, { onSuccess: done('classes.added'), onError });
    }
  };

  return (
    <>
      <FormSheetHeader
        title={
          editing ? t('classes.editTitle', { name: classLabel(editing) }) : t('classes.addTitle')
        }
        description={editing ? t('classes.editDescription') : t('classes.addDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          messages={{ 404: t('classes.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <Controller
          control={form.control}
          name="courseTypeId"
          render={({ field }) => (
            <SelectField
              label={t('classes.courseType')}
              placeholder={t('classes.choose')}
              options={options.courseTypes}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.courseTypeId)}
            />
          )}
        />
        <Controller
          control={form.control}
          name="roomId"
          render={({ field }) => (
            <SelectField
              label={t('classes.room')}
              placeholder={t('classes.choose')}
              options={options.rooms}
              value={field.value}
              onChange={pickRoom}
              onBlur={field.onBlur}
              error={errorText(errors.roomId)}
            />
          )}
        />
        <Controller
          control={form.control}
          name="instructorId"
          render={({ field }) => (
            <SelectField
              label={t('classes.instructor')}
              placeholder={t('classes.choose')}
              options={options.instructors}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.instructorId)}
            />
          )}
        />
        <Controller
          control={form.control}
          name="accompanistId"
          render={({ field }) => (
            <SelectField
              label={t('classes.accompanist')}
              placeholder={t('classes.noAccompanist')}
              options={options.accompanists}
              value={field.value || none}
              onChange={(value) => field.onChange(value === none ? '' : value)}
              onBlur={field.onBlur}
              error={errorText(errors.accompanistId)}
            />
          )}
        />
        <Controller
          control={form.control}
          name="level"
          render={({ field }) => (
            <SelectField
              label={t('classes.level')}
              placeholder={t('classes.choose')}
              options={options.levels}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.level)}
            />
          )}
        />
        <Controller
          control={form.control}
          name="weekday"
          render={({ field }) => (
            <SelectField
              label={t('classes.weekday')}
              placeholder={t('classes.choose')}
              options={options.weekdays}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.weekday)}
            />
          )}
        />
        <TextField
          label={t('classes.startTime')}
          type="time"
          step={60}
          hint={t('classes.startTimeHint')}
          error={errorText(errors.startTime)}
          {...form.register('startTime')}
        />
        <TextField
          label={t('classes.duration')}
          type="number"
          inputMode="numeric"
          min={1}
          max={1440}
          step={1}
          error={errorText(errors.durationMinutes)}
          {...form.register('durationMinutes')}
        />
        <TextField
          label={t('classes.capacity')}
          type="number"
          inputMode="numeric"
          min={1}
          max={1000}
          step={1}
          hint={editing ? t('classes.capacityHintEdit') : t('classes.capacityHint')}
          error={errorText(errors.capacity)}
          {...form.register('capacity')}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('classes.add')}
          icon={editing ? <Save aria-hidden="true" /> : <CalendarPlus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
