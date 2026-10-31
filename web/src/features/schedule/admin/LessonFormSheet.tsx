import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CalendarPlus, Save } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { LessonDetail } from '@/api/client';
import { conflictReason, roomTakenBy } from '@/api/conflict';
import {
  allStaffQuery,
  type CourseType,
  courseTypesQuery,
  createLessonMutation,
  type LessonCreateData,
  refusedAsStarted,
  type Location,
  locationsQuery,
  publicSettingsQuery,
  type Room,
  roomsQuery,
  type StaffEntry,
  updateLessonMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Skeleton } from '@/components/ui/skeleton';
import { SelectField } from '@/features/admin/crud/fields';
import { showServerErrors, useAdminErrorText } from '@/features/admin/crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from '@/features/admin/crud/FormSheet';
import { LoadError } from '@/features/admin/crud/LoadError';
import { roomOptions, staffOptions } from '@/features/admin/crud/options';
import { invalidFields } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import { ConfirmNotice, RoomTakenNotice } from './ConfirmNotice';
import {
  isRental,
  kinds,
  lessonData,
  lessonFields,
  type LessonInput,
  type LessonOutput,
  lessonSchema,
  lessonValues,
  maxRenterLength,
  newLessonData,
  visibilities,
} from './lessonForms';

/** What the sheet edits: a new one-off lesson or an existing lesson. */
export type LessonFormTarget = { mode: 'add' } | { mode: 'edit'; lesson: LessonDetail };

/** What the office has confirmed of a write the API refused before. */
type Confirmations = Pick<LessonCreateData, 'confirmPast' | 'confirmRoomOverlap'>;

/** The select value of "no accompanist": Radix Select has no empty values. */
const none = 'none';

/** Which server-refused fields get a message of their own rather than the general one. */
const serverMessages: Partial<Record<keyof LessonInput, string>> = {
  capacity: 'lessonBookedCapacity',
  durationMinutes: 'lessonMidnight',
  accompanistId: 'accompanist',
};

/**
 * A sheet with the form of a new one-off lesson or of an existing lesson
 * (office); closed while `target` is null. `onCreated` gets the id of a new
 * lesson.
 */
export function LessonFormSheet({
  target,
  onClose,
  onCreated,
}: {
  target: LessonFormTarget | null;
  onClose: () => void;
  onCreated?: (lessonId: string) => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <LessonFormLoader
          key={shown.mode === 'edit' ? shown.lesson.id : 'add'}
          target={shown}
          onDone={onClose}
          onCreated={onCreated}
        />
      )}
    </FormSheet>
  );
}

/** Loads the catalogue and the school's time zone the form needs. */
function LessonFormLoader({
  target,
  onDone,
  onCreated,
}: {
  target: LessonFormTarget;
  onDone: () => void;
  onCreated?: (lessonId: string) => void;
}) {
  const { t } = useTranslation('admin');
  const queries = [
    useQuery(publicSettingsQuery),
    useQuery(courseTypesQuery),
    useQuery(roomsQuery),
    useQuery(locationsQuery),
    useQuery(allStaffQuery),
  ] as const;
  const [settings, courseTypes, rooms, locations, staff] = queries;
  const header = (
    <FormSheetHeader
      title={target.mode === 'edit' ? t('lessons.editTitle') : t('lessons.addTitle')}
      description={target.mode === 'add' ? t('lessons.addDescription') : undefined}
    />
  );

  if (queries.some((q) => q.isError)) {
    return (
      <>
        {header}
        <div className="px-4">
          <LoadError
            message={t('lessons.loadError')}
            onRetry={() => {
              for (const q of queries) if (q.isError) void q.refetch();
            }}
          />
        </div>
      </>
    );
  }
  if (!settings.data || !courseTypes.data || !rooms.data || !locations.data || !staff.data) {
    return (
      <>
        {header}
        <div className="space-y-4 px-4" aria-hidden="true">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      </>
    );
  }
  return (
    <LessonForm
      target={target}
      timeZone={settings.data.timezone}
      choices={{
        courseTypes: courseTypes.data,
        rooms: rooms.data,
        locations: locations.data,
        staff: staff.data,
      }}
      onDone={onDone}
      onCreated={onCreated}
    />
  );
}

/** The catalogue the form offers. */
type Choices = {
  courseTypes: CourseType[];
  rooms: Room[];
  locations: Location[];
  staff: StaffEntry[];
};

function LessonForm({
  target,
  timeZone,
  choices,
  onDone,
  onCreated,
}: {
  target: LessonFormTarget;
  timeZone: string;
  choices: Choices;
  onDone: () => void;
  onCreated?: (lessonId: string) => void;
}) {
  const { t, i18n } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const { t: tSchedule } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createLessonMutation(queryClient));
  const update = useMutation(updateLessonMutation(queryClient));
  const editing = target.mode === 'edit' ? target.lesson : undefined;
  const save = editing ? update : create;
  const form = useForm<LessonInput, unknown, LessonOutput>({
    resolver: zodResolver(lessonSchema),
    defaultValues: lessonValues(editing, timeZone),
  });
  const { errors } = form.formState;
  const rental = isRental(useWatch({ control: form.control, name: 'kind' }));
  // The confirmations and the room of the last try.
  const [confirmed, setConfirmed] = useState<Confirmations>({});
  const [sentRoomId, setSentRoomId] = useState('');
  /** What a 422 means: the lesson has started, or the fields it marks need a look. */
  const problem422 = (error: unknown) =>
    refusedAsStarted(error, 'path.lessonId')
      ? { 422: t('lessons.notChangeable') }
      : invalidFields(error, lessonFields).length > 0
        ? { 422: tAuth('form.checkEntries') }
        : {};

  const options = useMemo(() => {
    const inactive = (name: string) => t('classes.inactiveStaff', { name });
    return {
      courseTypes: choices.courseTypes.map((c) => ({ value: c.id, label: c.name })),
      rooms: roomOptions(choices.rooms, choices.locations, lang, t('classes.unknown')),
      instructors: staffOptions(choices.staff, 'instructor', editing?.instructor, inactive),
      accompanists: [
        { value: none, label: t('lessons.noAccompanist') },
        ...staffOptions(choices.staff, 'accompanist', editing?.accompanist, inactive),
      ],
      visibilities: visibilities.map((v) => ({ value: v, label: tSchedule(`visibility.${v}`) })),
      kinds: kinds.map((k) => ({ value: k, label: t(`lessons.kinds.${k}`) })),
    };
  }, [choices, editing, lang, t, tSchedule]);

  /** A new lesson gets the capacity of its room, unless the capacity was typed. */
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
   * Sends the form values with `confirmations`: the ones of the last try
   * plus the one the office just gave, so a lesson in the past whose room
   * is taken as well needs both.
   */
  const send = (values: LessonOutput, confirmations: Confirmations) => {
    setConfirmed(confirmations);
    setSentRoomId(values.roomId);
    const onError = (error: Error) => {
      showServerErrors(error, form, lessonFields);
      for (const field of invalidFields(error, lessonFields)) {
        const message = serverMessages[field];
        if (message) form.setError(field, { type: 'server', message });
      }
    };
    if (editing) {
      update.mutate(
        { lessonId: editing.id, body: { ...lessonData(values), ...confirmations } },
        {
          onSuccess: () => {
            toast.success(t('lessons.saved'));
            onDone();
          },
          onError,
        },
      );
    } else {
      create.mutate(
        { ...newLessonData(values), ...confirmations },
        {
          onSuccess: (lesson) => {
            toast.success(t('lessons.added'));
            onDone();
            onCreated?.(lesson.id);
          },
          onError,
        },
      );
    }
  };

  const onSubmit = (values: LessonOutput) => send(values, {});
  /** Sends the current form values again, checked, with one more confirmation. */
  const confirmAnd = (confirmation: keyof Confirmations) => () =>
    void form.handleSubmit((values) => send(values, { ...confirmed, [confirmation]: true }))();

  const taken = roomTakenBy(save.error);
  const description = editing
    ? [
        t('lessons.editDescription'),
        editing.classId && !editing.admin?.detached ? t('lessons.editDetaches') : '',
        editing.status === 'scheduled' && (editing.admin?.activeBookings ?? 0) > 0
          ? t('lessons.editMails')
          : '',
      ]
        .filter(Boolean)
        .join(' ')
    : undefined;

  return (
    <>
      <FormSheetHeader
        title={editing ? t('lessons.editTitle') : t('lessons.addTitle')}
        description={editing ? description : t('lessons.addDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        {taken ? (
          <RoomTakenNotice
            room={choices.rooms.find((r) => r.id === sentRoomId)?.name ?? ''}
            lesson={taken}
            timeZone={timeZone}
            action={t('lessons.saveAnyway')}
            pending={save.isPending}
            onConfirm={confirmAnd('confirmRoomOverlap')}
          />
        ) : conflictReason(save.error) === 'past' ? (
          <ConfirmNotice
            title={t('lessons.past')}
            action={t('lessons.saveAnyway')}
            pending={save.isPending}
            onConfirm={confirmAnd('confirmPast')}
          >
            {t('lessons.pastDescription')}
          </ConfirmNotice>
        ) : (
          <ProblemAlert
            error={save.error}
            messages={{
              404: t('lessons.notFound'),
              ...problem422(save.error),
            }}
          />
        )}
        {editing ? (
          <p className="text-sm">
            <span className="text-muted-foreground">{t('lessons.courseType')}: </span>
            <span className="font-medium">{editing.courseType.name}</span>
          </p>
        ) : (
          <Controller
            control={form.control}
            name="courseTypeId"
            render={({ field }) => (
              <SelectField
                label={t('lessons.courseType')}
                placeholder={t('lessons.choose')}
                options={options.courseTypes}
                value={field.value}
                onChange={field.onChange}
                onBlur={field.onBlur}
                error={errorText(errors.courseTypeId)}
              />
            )}
          />
        )}
        <Controller
          control={form.control}
          name="kind"
          render={({ field }) => (
            <SelectField
              label={t('lessons.kind')}
              placeholder={t('lessons.choose')}
              options={options.kinds}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.kind)}
            />
          )}
        />
        {rental && (
          <>
            <p className="text-sm text-muted-foreground">{t('lessons.rentalHint')}</p>
            <TextField
              label={t('lessons.renter')}
              hint={t('lessons.renterHint')}
              maxLength={maxRenterLength}
              autoComplete="off"
              error={errorText(errors.renter)}
              {...form.register('renter')}
            />
          </>
        )}
        <TextField
          label={t('lessons.date')}
          type="date"
          hint={t('lessons.timeHint')}
          error={errorText(errors.date)}
          {...form.register('date')}
        />
        <TextField
          label={t('lessons.startTime')}
          type="time"
          step={60}
          error={errorText(errors.startTime)}
          {...form.register('startTime')}
        />
        <TextField
          label={t('lessons.duration')}
          type="number"
          inputMode="numeric"
          min={1}
          max={1440}
          step={1}
          error={errorText(errors.durationMinutes)}
          {...form.register('durationMinutes')}
        />
        <Controller
          control={form.control}
          name="roomId"
          render={({ field }) => (
            <SelectField
              label={t('lessons.room')}
              placeholder={t('lessons.choose')}
              options={options.rooms}
              value={field.value}
              onChange={pickRoom}
              onBlur={field.onBlur}
              error={errorText(errors.roomId)}
            />
          )}
        />
        {/* A room rental has no staff, no places and is hidden (ADR 0007, amendment). */}
        {!rental && (
          <>
            <Controller
              control={form.control}
              name="instructorId"
              render={({ field }) => (
                <SelectField
                  label={t('lessons.instructor')}
                  placeholder={t('lessons.choose')}
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
                  label={t('lessons.accompanist')}
                  placeholder={t('lessons.noAccompanist')}
                  options={options.accompanists}
                  value={field.value || none}
                  onChange={(value) => field.onChange(value === none ? '' : value)}
                  onBlur={field.onBlur}
                  error={errorText(errors.accompanistId)}
                />
              )}
            />
            <TextField
              label={t('lessons.capacity')}
              type="number"
              inputMode="numeric"
              min={1}
              max={1000}
              step={1}
              hint={editing ? t('lessons.capacityHintEdit') : t('lessons.capacityHint')}
              error={errorText(errors.capacity)}
              {...form.register('capacity')}
            />
            <Controller
              control={form.control}
              name="visibility"
              render={({ field }) => (
                <SelectField
                  label={t('lessons.visibility')}
                  placeholder={t('lessons.choose')}
                  options={options.visibilities}
                  value={field.value}
                  onChange={field.onChange}
                  onBlur={field.onBlur}
                  error={errorText(errors.visibility)}
                />
              )}
            />
          </>
        )}
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? t('form.save') : t('lessons.add')}
          icon={editing ? <Save aria-hidden="true" /> : <CalendarPlus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
