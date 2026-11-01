import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Ban, Eye, EyeOff, Pencil, RotateCcw, Trash2 } from 'lucide-react';
import { type ReactNode, useId, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { LessonDetail } from '@/api/client';
import { roomTakenBy } from '@/api/conflict';
import {
  cancelLessonMutation,
  deleteLessonMutation,
  refusedAsStarted,
  restoreLessonMutation,
  updateLessonMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import { DeleteDialog } from '@/features/admin/crud/DeleteDialog';
import { TextareaField } from '@/features/admin/crud/fields';
import { showServerErrors, useAdminErrorText } from '@/features/admin/crud/form';
import { invalidFields } from '@/features/auth/form';
import { formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';
import { useNow } from '@/lib/useNow';
import { useLessonTitle } from '../lessonLabel';
import { RoomTakenNotice } from './ConfirmNotice';
import { cancelSchema, type CancelValues, lessonDataOf } from './lessonForms';
import { LessonFormSheet } from './LessonFormSheet';

/** The dialog of an action that is open, if any. */
type Open = 'edit' | 'hide' | 'cancel' | 'restore' | 'delete' | null;

/**
 * The office's section of the lesson sheet: the active bookings, whether
 * the lesson still follows its class, and the actions (edit, hide or
 * unhide, cancel or restore, delete a one-off lesson). Nothing can be
 * changed once the lesson has started.
 */
export function LessonActions({
  lesson,
  timeZone,
  onDeleted,
}: {
  lesson: LessonDetail;
  timeZone: string;
  /** Called once the lesson is deleted, e.g. to close the sheet. */
  onDeleted: () => void;
}) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const queryClient = useQueryClient();
  const [open, setOpen] = useState<Open>(null);
  const visibility = useMutation(updateLessonMutation(queryClient));
  const headingId = useId();
  const title = useLessonTitle();

  const started = useNow() >= Date.parse(lesson.startsAt);
  const canceled = lesson.status === 'canceled';
  const hidden = lesson.visibility === 'hidden';
  const rental = lesson.kind === 'rental';
  const bookings = lesson.admin?.activeBookings ?? 0;
  const label = t('lessons.label', {
    courseType: title(lesson),
    day: formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang),
    time: formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang),
  });

  const setVisibility = (next: LessonDetail['visibility']) =>
    visibility.mutate(
      { lessonId: lesson.id, body: lessonDataOf(lesson, timeZone, { visibility: next }) },
      {
        onSuccess: () => {
          toast.success(next === 'hidden' ? t('lessons.hidden') : t('lessons.unhidden'));
          setOpen(null);
        },
      },
    );

  const onHide = () => {
    visibility.reset();
    if (hidden) setVisibility('public');
    else if (bookings > 0) setOpen('hide');
    else setVisibility('hidden');
  };

  return (
    <section aria-labelledby={headingId} className="space-y-3 rounded-lg border p-3">
      <h3 id={headingId} className="font-medium">
        {t('lessons.manage')}
      </h3>
      <ul className="space-y-1 text-sm text-muted-foreground">
        {lesson.admin && !rental && (
          <li>
            {bookings > 0 ? t('lessons.bookings', { count: bookings }) : t('lessons.noBookings')}
          </li>
        )}
        {lesson.classId && lesson.admin?.detached && <li>{t('lessons.detached')}</li>}
        {started && <li className="font-medium text-foreground">{t('lessons.started')}</li>}
      </ul>
      {open === null && (
        <ProblemAlert
          error={visibility.error}
          messages={{
            404: t('lessons.notFound'),
            ...(refusedAsStarted(visibility.error, 'path.lessonId')
              ? { 422: t('lessons.notChangeable') }
              : {}),
          }}
        />
      )}
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" disabled={started} onClick={() => setOpen('edit')}>
          <Pencil aria-hidden="true" />
          {t('lessons.edit')}
        </Button>
        {/* A room rental is always hidden (ADR 0007, amendment). */}
        {!rental && (
          <Button
            variant="outline"
            size="sm"
            disabled={started || visibility.isPending}
            onClick={onHide}
          >
            {hidden ? <Eye aria-hidden="true" /> : <EyeOff aria-hidden="true" />}
            {hidden ? t('lessons.unhide') : t('lessons.hide')}
          </Button>
        )}
        {canceled ? (
          <Button variant="outline" size="sm" disabled={started} onClick={() => setOpen('restore')}>
            <RotateCcw aria-hidden="true" />
            {t('lessons.restore')}
          </Button>
        ) : (
          <Button variant="outline" size="sm" disabled={started} onClick={() => setOpen('cancel')}>
            <Ban aria-hidden="true" />
            {t('lessons.cancel')}
          </Button>
        )}
        {!lesson.classId && (
          <Button variant="outline" size="sm" disabled={started} onClick={() => setOpen('delete')}>
            <Trash2 aria-hidden="true" />
            {t('lessons.delete')}
          </Button>
        )}
      </div>

      <LessonFormSheet
        target={open === 'edit' ? { mode: 'edit', lesson } : null}
        onClose={() => setOpen(null)}
      />
      <ConfirmAction
        open={open === 'hide'}
        title={t('lessons.hideTitle')}
        description={t('lessons.hideDescription', { count: bookings })}
        confirm={visibility.isPending ? t('lessons.hiding') : t('lessons.hideConfirm')}
        dismiss={t('lessons.back')}
        pending={visibility.isPending}
        error={visibility.error}
        onConfirm={() => setVisibility('hidden')}
        onClose={() => setOpen(null)}
      />
      <CancelDialog
        key={`cancel-${String(open === 'cancel')}`}
        open={open === 'cancel'}
        lesson={lesson}
        onClose={() => setOpen(null)}
      />
      <RestoreDialog
        key={`restore-${String(open === 'restore')}`}
        open={open === 'restore'}
        lesson={lesson}
        timeZone={timeZone}
        onClose={() => setOpen(null)}
      />
      <DeleteDialog
        target={open === 'delete' ? lesson : null}
        id={(l) => l.id}
        mutation={deleteLessonMutation}
        texts={() => ({
          title: t('lessons.deleteTitle'),
          description: t('lessons.deleteDescription', { lesson: label }),
          inUse: t('lessons.inUse'),
          notFound: t('lessons.notFound'),
          deleted: t('lessons.deleted'),
          conflicts: { generated: t('lessons.generated') },
        })}
        onDeleted={onDeleted}
        onClose={() => setOpen(null)}
      />
    </section>
  );
}

/**
 * Asks before an action on the lesson; a refused action keeps the dialog
 * open and says why: a 422 at the lesson means it has started meanwhile. `children` go between the description and the buttons
 * (e.g. a form whose submit button is the confirm button, via `form`).
 */
function ConfirmAction({
  open,
  title,
  description,
  confirm,
  dismiss,
  pending,
  error,
  fields = [],
  destructive = false,
  form,
  children,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  description: string;
  confirm: string;
  dismiss: string;
  pending: boolean;
  error: unknown;
  fields?: readonly string[];
  destructive?: boolean;
  /** The id of the form the confirm button submits; without one, it calls onConfirm. */
  form?: string;
  children?: ReactNode;
  onConfirm?: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <ProblemAlert
          error={error}
          messages={{
            404: t('lessons.notFound'),
            // A 422 at the lesson means it has started; one at a field, that it needs a look.
            ...(refusedAsStarted(error, 'path.lessonId')
              ? { 422: t('lessons.notChangeable') }
              : invalidFields(error, fields).length > 0
                ? { 422: tAuth('form.checkEntries') }
                : {}),
          }}
        />
        {children}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{dismiss}</AlertDialogCancel>
          {/* No AlertDialogAction: it would close the dialog before the answer. */}
          <Button
            variant={destructive ? 'destructive' : 'default'}
            disabled={pending}
            type={form ? 'submit' : 'button'}
            form={form}
            onClick={form ? undefined : onConfirm}
          >
            {confirm}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** Cancels the lesson with an optional note for those booked. */
function CancelDialog({
  open,
  lesson,
  onClose,
}: {
  open: boolean;
  lesson: LessonDetail;
  onClose: () => void;
}) {
  const { t } = useTranslation('admin');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const cancel = useMutation(cancelLessonMutation(queryClient));
  const formId = useId();
  const form = useForm<CancelValues>({
    resolver: zodResolver(cancelSchema),
    defaultValues: { note: '' },
  });
  const bookings = lesson.admin?.activeBookings ?? 0;

  const onSubmit = ({ note }: CancelValues) =>
    cancel.mutate(
      { lessonId: lesson.id, body: note ? { note } : {} },
      {
        onSuccess: () => {
          toast.success(t('lessons.canceled'));
          onClose();
        },
        onError: (error) => showServerErrors(error, form, ['note']),
      },
    );

  return (
    <ConfirmAction
      open={open}
      title={t('lessons.cancelTitle')}
      description={
        bookings > 0
          ? t('lessons.cancelDescription', { count: bookings })
          : t('lessons.cancelUnbooked')
      }
      confirm={cancel.isPending ? t('lessons.canceling') : t('lessons.cancel')}
      dismiss={t('lessons.keep')}
      pending={cancel.isPending}
      error={cancel.error}
      fields={['note']}
      destructive
      form={formId}
      onClose={onClose}
    >
      <form id={formId} noValidate onSubmit={form.handleSubmit(onSubmit)}>
        <TextareaField
          label={t('lessons.noteLabel')}
          hint={t('lessons.noteHint')}
          rows={3}
          error={errorText(form.formState.errors.note)}
          {...form.register('note')}
        />
      </form>
    </ConfirmAction>
  );
}

/**
 * Lets the canceled lesson take place after all. If another lesson has
 * taken its room meanwhile, it names that lesson and restores anyway on
 * request.
 */
function RestoreDialog({
  open,
  lesson,
  timeZone,
  onClose,
}: {
  open: boolean;
  lesson: LessonDetail;
  timeZone: string;
  onClose: () => void;
}) {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const restore = useMutation(restoreLessonMutation(queryClient));
  const bookings = lesson.admin?.activeBookings ?? 0;
  const taken = roomTakenBy(restore.error);
  const send = (confirmRoomOverlap: boolean) =>
    restore.mutate(
      { lessonId: lesson.id, body: confirmRoomOverlap ? { confirmRoomOverlap } : {} },
      {
        onSuccess: () => {
          toast.success(t('lessons.restored'));
          onClose();
        },
      },
    );
  return (
    <ConfirmAction
      open={open}
      title={t('lessons.restoreTitle')}
      description={
        bookings > 0
          ? t('lessons.restoreDescription', { count: bookings })
          : t('lessons.restoreUnbooked')
      }
      confirm={restore.isPending ? t('lessons.restoring') : t('lessons.restoreConfirm')}
      dismiss={t('lessons.back')}
      pending={restore.isPending}
      error={taken ? null : restore.error}
      onConfirm={() => send(false)}
      onClose={onClose}
    >
      {taken && (
        <RoomTakenNotice
          room={lesson.room.name}
          lesson={taken}
          timeZone={timeZone}
          action={t('lessons.restoreAnyway')}
          pending={restore.isPending}
          onConfirm={() => send(true)}
        />
      )}
    </ConfirmAction>
  );
}
