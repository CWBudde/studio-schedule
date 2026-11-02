import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarPlus, Save } from 'lucide-react';
import { useMemo } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type ClassEntry,
  type CourseType,
  createClassMutation,
  levelsQuery,
  type Location,
  type Room,
  type StaffEntry,
  updateClassMutation,
} from '@/api/queries';
import { invalidFields } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import { weekdayName } from '@/features/timetable/timetable';
import {
  allLevels,
  classData,
  classFields,
  type ClassInput,
  type ClassOutput,
  classSchema,
  classValues,
  weekdaysMondayFirst,
} from './classForms';
import { SelectField } from './crud/fields';
import { roomOptions, staffOptions } from './crud/options';
import { useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { ProblemAlert } from '@/components/ProblemAlert';
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
 * Which server-refused request body fields get a message of their own rather
 * than the general one. A level the server does not know was deleted meanwhile.
 */
const serverMessages: Partial<Record<keyof typeof classFields, string>> = {
  capacity: 'bookedCapacity',
  durationMinutes: 'midnight',
  accompanistId: 'accompanist',
  levelFromId: 'level',
  levelToId: 'level',
};

const bodyFields = Object.keys(classFields) as (keyof typeof classFields)[];

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
  const lang = i18n.resolvedLanguage ?? 'en';
  const errorText = useAdminErrorText();
  const classLabel = useClassLabel(choices.courseTypes);
  const queryClient = useQueryClient();
  const create = useMutation(createClassMutation(queryClient));
  const update = useMutation(updateClassMutation(queryClient));
  const levels = useQuery(levelsQuery);
  const editing = target.mode === 'edit' ? target.entry : undefined;
  const save = editing ? update : create;
  const form = useForm<ClassInput, unknown, ClassOutput>({
    resolver: zodResolver(classSchema),
    defaultValues: classValues(editing),
  });
  const { errors } = form.formState;
  const level = useWatch({ control: form.control, name: 'level' });
  const spans = level !== '' && level !== allLevels;
  const noLevels = levels.data?.length === 0;

  const options = useMemo(() => {
    const inactive = (name: string) => t('classes.inactiveStaff', { name });
    return {
      courseTypes: choices.courseTypes.map((c) => ({ value: c.id, label: c.name })),
      rooms: roomOptions(choices.rooms, choices.locations, t('classes.unknown')),
      instructors: staffOptions(choices.staff, 'instructor', editing?.instructor, inactive),
      accompanists: [
        { value: none, label: t('classes.noAccompanist') },
        ...staffOptions(choices.staff, 'accompanist', editing?.accompanist, inactive),
      ],
      levels: (levels.data ?? []).map((l) => ({ value: l.id, label: l.name })),
      weekdays: weekdaysMondayFirst.map((d) => ({
        value: String(d),
        label: weekdayName(d, lang),
      })),
    };
  }, [choices, editing, levels.data, lang, t]);

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

  /**
   * Picks the level the class is for, or none, or all. "Up to" follows a
   * span of one level and is cleared without a level.
   */
  const pickLevel = (value: string) => {
    const shouldValidate = form.formState.isSubmitted;
    const [was, to] = form.getValues(['level', 'levelTo']);
    const picked = value === none ? '' : value;
    form.setValue('level', picked, { shouldValidate });
    const isLevel = picked !== '' && picked !== allLevels;
    form.setValue('levelTo', isLevel ? (to === '' || to === was ? picked : to) : '', {
      shouldValidate,
    });
  };

  const onSubmit = (values: ClassOutput) => {
    const body = classData(values, editing?.retired ?? false);
    const name = classLabel(values);
    const onError = (error: Error) => {
      const refused = invalidFields(error, bodyFields);
      for (const field of refused) {
        form.setError(classFields[field], {
          type: 'server',
          message: serverMessages[field] ?? 'invalid',
        });
      }
      if (refused.includes('levelFromId') || refused.includes('levelToId')) {
        void queryClient.invalidateQueries({ queryKey: levelsQuery.queryKey });
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
        {noLevels ? (
          <div className="grid gap-1.5">
            <p className="text-sm leading-none font-medium">{t('classes.level')}</p>
            <p className="text-sm text-muted-foreground">
              {t('classes.noLevels')}{' '}
              <Link
                to="/admin/levels"
                className="font-medium text-primary underline-offset-4 hover:underline"
              >
                {t('classes.noLevelsLink')}
              </Link>
            </p>
          </div>
        ) : (
          <>
            <Controller
              control={form.control}
              name="level"
              render={({ field }) => (
                <SelectField
                  label={t('classes.level')}
                  placeholder={t('classes.noLevel')}
                  options={[
                    { value: none, label: t('classes.noLevel') },
                    { value: allLevels, label: t('classes.allLevels') },
                    ...options.levels,
                  ]}
                  value={field.value || none}
                  onChange={pickLevel}
                  onBlur={field.onBlur}
                  error={errorText(errors.level)}
                />
              )}
            />
            {spans && (
              <Controller
                control={form.control}
                name="levelTo"
                render={({ field }) => (
                  <SelectField
                    label={t('classes.levelTo')}
                    placeholder={t('classes.choose')}
                    options={options.levels}
                    value={field.value}
                    onChange={field.onChange}
                    onBlur={field.onBlur}
                    error={errorText(errors.levelTo)}
                    hint={t('classes.levelToHint')}
                  />
                )}
              />
            )}
          </>
        )}
        <TextField
          label={t('classes.stage')}
          type="number"
          inputMode="numeric"
          min={1}
          max={9}
          step={1}
          hint={t('classes.stageHint')}
          error={errorText(errors.stage)}
          {...form.register('stage')}
        />
        <div className="grid grid-cols-2 gap-4">
          <TextField
            label={t('classes.ageFrom')}
            type="number"
            inputMode="decimal"
            min={0.5}
            max={100}
            step={0.5}
            hint={t('classes.ageHint')}
            error={errorText(errors.ageFrom)}
            {...form.register('ageFrom')}
          />
          <TextField
            label={t('classes.ageTo')}
            type="number"
            inputMode="decimal"
            min={0.5}
            max={100}
            step={0.5}
            hint={t('classes.ageHint')}
            error={errorText(errors.ageTo)}
            {...form.register('ageTo')}
          />
        </div>
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
