import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Megaphone, Save } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Controller, type FieldError, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  allClassesQuery,
  type Announcement,
  type AnnouncementData,
  announcementRecipientsQuery,
  courseTypesQuery,
  createAnnouncementMutation,
  scheduleQuery,
  termsQuery,
  updateAnnouncementMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { TextField } from '@/features/auth/TextField';
import { SelectField, SwitchField, TextareaField } from '@/features/admin/crud/fields';
import { showServerErrors, useAdminErrorText } from '@/features/admin/crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from '@/features/admin/crud/FormSheet';
import { useClassLabel } from '@/features/admin/useClassLabel';
import { invalidFields } from '@/features/auth/form';
import { addDays, formatTimestamp, formatTime } from '@/lib/schoolTime';
import {
  announcementFields,
  type AnnouncementValues,
  announcementSchema,
  announcementValues,
  audienceOf,
  audiences,
  dateFields,
  mailStarted,
  roles,
} from './announcementForms';

/** What the sheet edits: a new announcement or an existing one. */
export type AnnouncementTarget = { mode: 'add' } | { mode: 'edit'; announcement: Announcement };

/** Error keys of the announcement form beyond the admin forms' own. */
const ownErrors = ['dateTime', 'expiresBeforePublish', 'titleLine'] as const;

/** Translates a field error of the form: its own keys, else the admin forms'. */
function useErrorText() {
  const { t } = useTranslation('announcements');
  const adminText = useAdminErrorText();
  return (error: FieldError | undefined) =>
    (ownErrors as readonly string[]).includes(error?.message ?? '')
      ? t(`admin.errors.${error?.message as (typeof ownErrors)[number]}`)
      : adminText(error);
}

/**
 * A sheet with the form of a new or an existing announcement, its dates and
 * times in the school's time zone; closed while `target` is null.
 */
export function AnnouncementSheet({
  target,
  timeZone,
  onClose,
}: {
  target: AnnouncementTarget | null;
  timeZone: string;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => (
        <AnnouncementForm
          key={shown.mode === 'edit' ? shown.announcement.id : 'add'}
          target={shown}
          timeZone={timeZone}
          onDone={onClose}
        />
      )}
    </FormSheet>
  );
}

function AnnouncementForm({
  target,
  timeZone,
  onDone,
}: {
  target: AnnouncementTarget;
  timeZone: string;
  onDone: () => void;
}) {
  const { t, i18n } = useTranslation('announcements');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const lang = i18n.resolvedLanguage ?? 'en';
  const errorText = useErrorText();
  const queryClient = useQueryClient();
  const create = useMutation(createAnnouncementMutation(queryClient));
  const update = useMutation(updateAnnouncementMutation(queryClient));
  const editing = target.mode === 'edit' ? target.announcement : undefined;
  const emailStarted = editing ? mailStarted(editing) : false;
  const save = editing ? update : create;
  const schema = useMemo(() => announcementSchema(timeZone), [timeZone]);
  const form = useForm<AnnouncementValues, unknown, AnnouncementData>({
    resolver: zodResolver(schema),
    defaultValues: announcementValues(timeZone, editing),
  });
  const { errors } = form.formState;
  const [audience, termId, lessonDate] = useWatch({
    control: form.control,
    name: ['audience', 'termId', 'lessonDate'],
  });
  /** The body waiting for the confirmation of its email. */
  const [confirming, setConfirming] = useState<AnnouncementData | null>(null);

  const send = (body: AnnouncementData) => {
    const onError = (error: Error) => {
      showServerErrors(error, form, announcementFields);
      for (const [api, field] of Object.entries(dateFields)) {
        if (invalidFields(error, [api]).length > 0) {
          form.setError(field, { type: 'server', message: 'invalid' });
        }
      }
    };
    const done =
      (message: 'admin.added' | 'admin.addedSending' | 'admin.scheduled' | 'admin.saved') => () => {
        toast.success(t(message, { title: body.title }));
        onDone();
      };
    if (editing) {
      update.mutate(
        { announcementId: editing.id, body },
        { onSuccess: done('admin.saved'), onError },
      );
    } else {
      create.mutate(body, {
        // Published later than written: scheduled. Published now with
        // email: the announcements job is sending it.
        onSuccess: (a) =>
          done(
            Date.parse(a.publishAt ?? a.createdAt) > Date.parse(a.createdAt)
              ? 'admin.scheduled'
              : a.email
                ? 'admin.addedSending'
                : 'admin.added',
          )(),
        onError,
      });
    }
  };

  const onSubmit = (body: AnnouncementData) => {
    if (body.email && !emailStarted) setConfirming(body);
    else send(body);
  };

  return (
    <>
      <FormSheetHeader
        title={editing ? t('admin.editTitle', { title: editing.title }) : t('admin.addTitle')}
        description={t('admin.sheetDescription')}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={save.error}
          conflicts={{ 'mail-limit': t('admin.mailLimit'), expired: t('admin.expired') }}
          messages={{ 404: t('admin.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <TextField
          label={t('admin.titleField')}
          autoComplete="off"
          error={errorText(errors.title)}
          {...form.register('title')}
        />
        <TextareaField
          label={t('admin.body')}
          rows={6}
          hint={t('admin.bodyHint')}
          error={errorText(errors.body)}
          {...form.register('body')}
        />
        <Controller
          control={form.control}
          name="audience"
          render={({ field }) => (
            <SelectField
              label={t('admin.audience')}
              placeholder={t('admin.choose')}
              options={audiences.map((a) => ({ value: a, label: t(`admin.audiences.${a}`) }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.audience)}
            />
          )}
        />
        {audience === 'role' && (
          <Controller
            control={form.control}
            name="role"
            render={({ field }) => (
              <SelectField
                label={t('admin.role')}
                placeholder={t('admin.choose')}
                options={roles.map((r) => ({ value: r, label: t(`admin.roles.${r}`) }))}
                value={field.value}
                onChange={field.onChange}
                onBlur={field.onBlur}
                error={errorText(errors.role)}
              />
            )}
          />
        )}
        {audience === 'class' && (
          <ClassFields
            termId={termId}
            form={form}
            errorText={errorText}
            onTermChange={() => form.setValue('classId', '')}
          />
        )}
        {audience === 'course-type' && <CourseTypeField form={form} errorText={errorText} />}
        {audience === 'lesson' && (
          <LessonFields
            date={lessonDate}
            timeZone={timeZone}
            lang={lang}
            form={form}
            errorText={errorText}
          />
        )}
        <div className="grid grid-cols-[1fr_auto] items-start gap-2">
          <TextField
            label={t('admin.publishDate')}
            type="date"
            hint={t('admin.publishHint')}
            error={errorText(errors.publishDate)}
            {...form.register('publishDate')}
          />
          <TextField label={t('admin.publishTime')} type="time" {...form.register('publishTime')} />
        </div>
        <div className="grid grid-cols-[1fr_auto] items-start gap-2">
          <TextField
            label={t('admin.expiresDate')}
            type="date"
            hint={t('admin.expiresHint')}
            error={errorText(errors.expiresDate)}
            {...form.register('expiresDate')}
          />
          <TextField label={t('admin.expiresTime')} type="time" {...form.register('expiresTime')} />
        </div>
        <Controller
          control={form.control}
          name="email"
          render={({ field }) => (
            <SwitchField
              label={t('admin.emailField')}
              hint={
                editing?.emailedAt
                  ? t('admin.emailSent', {
                      date: formatTimestamp(editing.emailedAt, timeZone, lang),
                    })
                  : emailStarted
                    ? t('admin.emailSendingHint')
                    : t('admin.emailHint')
              }
              checked={field.value}
              disabled={emailStarted}
              onChange={field.onChange}
              onBlur={field.onBlur}
            />
          )}
        />
        <FormSheetActions
          pending={save.isPending}
          submitLabel={editing ? tAdmin('form.save') : t('admin.publish')}
          icon={editing ? <Save aria-hidden="true" /> : <Megaphone aria-hidden="true" />}
        />
      </form>
      <ConfirmEmail
        body={confirming}
        onCancel={() => setConfirming(null)}
        onConfirm={(body) => {
          setConfirming(null);
          send(body);
        }}
      />
    </>
  );
}

type FormProps = {
  form: ReturnType<typeof useForm<AnnouncementValues, unknown, AnnouncementData>>;
  errorText: (error: FieldError | undefined) => string | undefined;
};

function ClassFields({
  termId,
  form,
  errorText,
  onTermChange,
}: FormProps & { termId: string; onTermChange: () => void }) {
  const { t } = useTranslation('announcements');
  const terms = useQuery(termsQuery);
  const courseTypes = useQuery(courseTypesQuery);
  const classes = useQuery({ ...allClassesQuery(termId), enabled: termId !== '' });
  const classLabel = useClassLabel(courseTypes.data ?? []);
  const { errors } = form.formState;
  return (
    <>
      <Controller
        control={form.control}
        name="termId"
        render={({ field }) => (
          <SelectField
            label={t('admin.term')}
            placeholder={t('admin.choose')}
            options={(terms.data ?? []).map((term) => ({ value: term.id, label: term.name }))}
            value={field.value}
            onChange={(value) => {
              field.onChange(value);
              onTermChange();
            }}
            onBlur={field.onBlur}
          />
        )}
      />
      <Controller
        control={form.control}
        name="classId"
        render={({ field }) => (
          <SelectField
            label={t('admin.class')}
            placeholder={t('admin.choose')}
            options={(classes.data ?? []).map((c) => ({ value: c.id, label: classLabel(c) }))}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.classId)}
          />
        )}
      />
    </>
  );
}

function CourseTypeField({ form, errorText }: FormProps) {
  const { t } = useTranslation('announcements');
  const courseTypes = useQuery(courseTypesQuery);
  return (
    <Controller
      control={form.control}
      name="courseTypeId"
      render={({ field }) => (
        <SelectField
          label={t('admin.courseType')}
          placeholder={t('admin.choose')}
          options={(courseTypes.data ?? []).map((c) => ({ value: c.id, label: c.name }))}
          value={field.value}
          onChange={field.onChange}
          onBlur={field.onBlur}
          error={errorText(form.formState.errors.courseTypeId)}
        />
      )}
    />
  );
}

function LessonFields({
  date,
  timeZone,
  lang,
  form,
  errorText,
}: FormProps & { date: string; timeZone: string; lang: string }) {
  const { t } = useTranslation('announcements');
  const valid = /^\d{4}-\d{2}-\d{2}$/.test(date);
  const schedule = useQuery({
    ...scheduleQuery({ from: date, to: valid ? addDays(date, 1) : date }),
    enabled: valid,
  });
  const lessons = valid ? (schedule.data?.lessons ?? []) : [];
  const { errors } = form.formState;
  return (
    <>
      <TextField
        label={t('admin.lessonDate')}
        type="date"
        {...form.register('lessonDate', { onChange: () => form.setValue('lessonId', '') })}
      />
      <Controller
        control={form.control}
        name="lessonId"
        render={({ field }) => (
          <SelectField
            label={t('admin.lesson')}
            placeholder={t('admin.choose')}
            hint={
              valid && schedule.isSuccess && lessons.length === 0 ? t('admin.noLessons') : undefined
            }
            options={lessons.map((l) => ({
              value: l.id,
              label: `${formatTime(l.startsAt, timeZone, lang)} ${l.courseType.name} (${l.room.name})`,
            }))}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.lessonId)}
          />
        )}
      />
    </>
  );
}

/**
 * Asks before an announcement goes out by email, with the number of
 * addresses it reaches now; closed while `body` is null.
 */
function ConfirmEmail({
  body,
  onCancel,
  onConfirm,
}: {
  body: AnnouncementData | null;
  onCancel: () => void;
  onConfirm: (body: AnnouncementData) => void;
}) {
  const { t } = useTranslation('announcements');
  const { t: tAdmin } = useTranslation('admin');
  const recipients = useQuery({
    ...announcementRecipientsQuery(body ? audienceOf(body) : { audience: 'everyone' }),
    enabled: body !== null,
  });
  return (
    <AlertDialog
      open={body !== null}
      onOpenChange={(open) => {
        if (!open) onCancel();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('admin.confirmTitle')}</AlertDialogTitle>
          <AlertDialogDescription>
            {recipients.isPending
              ? t('admin.confirmCounting')
              : recipients.isError
                ? t('admin.confirmFailed')
                : recipients.data === 0
                  ? t('admin.confirmNobody')
                  : t('admin.confirmCount', { count: recipients.data })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{tAdmin('form.cancel')}</AlertDialogCancel>
          <AlertDialogAction
            disabled={recipients.isPending}
            onClick={() => {
              if (body) onConfirm(body);
            }}
          >
            {t('admin.confirmSend')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
