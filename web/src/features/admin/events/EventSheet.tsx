import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type AdminEvent,
  type CourseType,
  type EventData,
  courseTypesQuery,
  createEventMutation,
  publicSettingsQuery,
  updateEventMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Skeleton } from '@/components/ui/skeleton';
import { TextField } from '@/features/auth/TextField';
import { currencyDigits } from '@/lib/money';
import { SelectField, TextareaField } from '../crud/fields';
import { showServerErrors, useAdminErrorText } from '../crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from '../crud/FormSheet';
import { LoadError } from '../crud/LoadError';
import {
  eventDateFields,
  eventFields,
  eventSchema,
  eventValues,
  type EventValues,
} from './eventForms';

export type EventTarget = { mode: 'add' } | { mode: 'edit'; event: AdminEvent };

export function EventSheet({
  target,
  onClose,
}: {
  target: EventTarget | null;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <EventFormLoader
          key={shown.mode === 'edit' ? shown.event.id : 'add'}
          event={shown.mode === 'edit' ? shown.event : undefined}
          onClose={onClose}
        />
      )}
    </FormSheet>
  );
}

function EventFormLoader({ event, onClose }: { event?: AdminEvent; onClose: () => void }) {
  const { t } = useTranslation('adminEvents');
  const settings = useQuery(publicSettingsQuery);
  const courseTypes = useQuery(courseTypesQuery);
  return (
    <>
      <FormSheetHeader
        title={event ? t('editName', { title: event.title }) : t('add')}
        description={settings.data && t('form.timeZone', { timeZone: settings.data.timezone })}
      />
      {settings.isError || courseTypes.isError ? (
        <div className="px-4">
          <LoadError
            message={t('loadError')}
            onRetry={() => {
              if (settings.isError) void settings.refetch();
              if (courseTypes.isError) void courseTypes.refetch();
            }}
          />
        </div>
      ) : settings.data && courseTypes.data ? (
        <EventForm
          event={event}
          currency={settings.data.currency}
          timeZone={settings.data.timezone}
          courseTypes={courseTypes.data}
          onClose={onClose}
        />
      ) : (
        <Skeleton className="mx-4 h-20" />
      )}
    </>
  );
}

function EventForm({
  event,
  currency,
  timeZone,
  courseTypes,
  onClose,
}: {
  event?: AdminEvent;
  currency: string;
  timeZone: string;
  courseTypes: CourseType[];
  onClose: () => void;
}) {
  const { t, i18n } = useTranslation('adminEvents');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createEventMutation(queryClient));
  const update = useMutation(updateEventMutation(queryClient));
  const save = event ? update : create;
  const form = useForm<EventValues, unknown, EventData>({
    resolver: zodResolver(eventSchema(currencyDigits(currency), timeZone, event)),
    defaultValues: eventValues(currency, i18n.resolvedLanguage ?? 'en', timeZone, event),
  });
  const { errors } = form.formState;
  const fixedCourseType = event && event.sessions.length > 0;
  const onSubmit = (body: EventData) => {
    const callbacks = {
      onSuccess: () => {
        toast.success(t(event ? 'form.saved' : 'form.created'));
        onClose();
      },
      onError: (error: Error) => showServerErrors(error, form, eventFields, 'slug'),
    };
    if (event) update.mutate({ eventId: event.id, body }, callbacks);
    else create.mutate(body, callbacks);
  };

  return (
    <form noValidate className="grid gap-4 px-4 pb-4" onSubmit={form.handleSubmit(onSubmit)}>
      <ProblemAlert
        error={save.error}
        messages={{ 404: t('notFound'), 422: tAuth('form.checkEntries') }}
        conflicts={{ duplicate: t('conflicts.duplicate') }}
      />
      <TextField
        label={t('form.title')}
        maxLength={200}
        error={errorText(errors.title)}
        {...form.register('title')}
      />
      <TextareaField
        label={t('form.description')}
        rows={4}
        maxLength={10_000}
        error={errorText(errors.description)}
        {...form.register('description')}
      />
      <TextField
        label={t('form.slug')}
        maxLength={80}
        autoComplete="off"
        hint={t('form.slugHint')}
        error={
          errors.slug?.message === 'slugInvalid' ? t('form.slugInvalid') : errorText(errors.slug)
        }
        {...form.register('slug')}
      />
      {fixedCourseType ? (
        <TextField
          label={t('form.courseType')}
          value={courseTypes.find((type) => type.id === event.courseTypeId)?.name ?? ''}
          readOnly
          hint={t('form.courseTypeHint')}
          error={errorText(errors.courseTypeId)}
        />
      ) : (
        <Controller
          control={form.control}
          name="courseTypeId"
          render={({ field }) => (
            <SelectField
              label={t('form.courseType')}
              placeholder={tAdmin('lessons.choose')}
              options={courseTypes.map(({ id, name }) => ({ value: id, label: name }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.courseTypeId)}
            />
          )}
        />
      )}
      <TextField
        label={t('form.capacity')}
        type="number"
        min={1}
        max={10_000}
        step={1}
        error={
          errors.capacity?.message === 'capacityInvalid'
            ? t('form.capacityInvalid')
            : errorText(errors.capacity)
        }
        {...form.register('capacity')}
      />
      <TextField
        label={t('form.price', { currency })}
        inputMode="decimal"
        error={errorText(errors.price)}
        {...form.register('price')}
      />
      <TextField
        label={t('form.vatRate')}
        inputMode="decimal"
        error={errorText(errors.vatRate)}
        {...form.register('vatRate')}
      />
      {eventDateFields.map((field) => (
        <TextField
          key={field}
          label={t(`form.${field}`)}
          type="datetime-local"
          step={60}
          hint={t(`form.${field}Hint`)}
          error={
            errors[field]?.message === 'signupOrder'
              ? t('form.signupOrder')
              : errorText(errors[field])
          }
          {...form.register(field)}
        />
      ))}
      <FormSheetActions
        pending={save.isPending}
        submitLabel={event ? tAdmin('form.save') : t('add')}
      />
    </form>
  );
}
