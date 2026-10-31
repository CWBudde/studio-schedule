import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Copy, Eye } from 'lucide-react';
import { type ReactNode, useId, useMemo, useState } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { LessonSummary } from '@/api/client';
import {
  cloneLessonsMutation,
  type LessonClone,
  type LessonCloneData,
  previewLessonCloneMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { SheetClose } from '@/components/ui/sheet';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { showServerErrors, useAdminErrorText } from '@/features/admin/crud/form';
import { FormSheet, FormSheetHeader } from '@/features/admin/crud/FormSheet';
import { invalidFields } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import {
  addDays,
  formatDayHeading,
  formatTimeRange,
  type SchoolDate,
  schoolDateOf,
  weekdayOf,
} from '@/lib/schoolTime';
import {
  cloneDefaults,
  cloneFields,
  type CloneInput,
  cloneSchema,
  type CloneUnit,
  earliestTarget,
  switchUnit,
} from './cloneForms';

/** What the schedule shows: its first day, whether as a list or a week, and today. */
export type CloneContext = {
  shown: SchoolDate;
  view: 'list' | 'week';
  today: SchoolDate;
  timeZone: string;
};

/**
 * The office's button to clone the lessons of a day or a week, with its
 * sheet: the form, the preview, then the clone and its result. The form
 * starts from the shown day or week.
 */
export function CloneLessons({ shown, view, today, timeZone }: CloneContext) {
  const { t } = useTranslation('admin');
  // Each opening is a fresh form, keyed by its number.
  const [opened, setOpened] = useState(0);
  const [open, setOpen] = useState(false);
  // FormSheet stores each new target during render: keep it stable until the sheet opens again.
  const target = useMemo(
    () => (open ? { opened, context: { shown, view, today, timeZone } } : null),
    [open, opened, shown, view, today, timeZone],
  );
  return (
    <>
      <Button
        variant="outline"
        onClick={() => {
          setOpened((n) => n + 1);
          setOpen(true);
        }}
      >
        <Copy aria-hidden="true" />
        {t('clone.open')}
      </Button>
      <FormSheet target={target} onClose={() => setOpen(false)}>
        {(shown) => <CloneForm key={shown.opened} {...shown.context} />}
      </FormSheet>
    </>
  );
}

/** Whether a request was for the values the form holds now. */
const sameRequest = (request: LessonCloneData | undefined, values: CloneInput) =>
  request !== undefined &&
  request.unit === values.unit &&
  request.source === values.source &&
  request.target === values.target;

/**
 * Who takes the room of a skip, the same in a preview and its clone: a copy
 * of the clone by the lesson it copies (only the clone gives it an id), any
 * other lesson by its id, and a lesson without one (e.g. a class lesson not
 * generated yet) by its course type and start.
 */
const takerOf = (lesson: LessonClone['skipped'][number]['lesson']) =>
  lesson && (lesson.copyOf ?? lesson.id ?? `${lesson.courseType.id}@${lesson.startsAt}`);

/** The source lessons a clone creates and skips, with the reason of each skip. */
const outline = (clone: LessonClone) =>
  JSON.stringify([
    clone.created.map((c) => c.source.id),
    clone.skipped.map((s) => [s.source.id, s.reason, takerOf(s.lesson)]),
  ]);

/** Whether a clone did otherwise than its preview said (the preview was stale). */
const differs = (planned: LessonClone | undefined, done: LessonClone) =>
  planned !== undefined && outline(planned) !== outline(done);

function CloneForm({ shown, view, today, timeZone }: CloneContext) {
  const { t, i18n } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const lang = i18n.resolvedLanguage ?? 'en';
  const errorText = useAdminErrorText();
  const unitLabel = useId();
  const queryClient = useQueryClient();
  const preview = useMutation(previewLessonCloneMutation);
  const apply = useMutation(cloneLessonsMutation(queryClient));
  const schema = useMemo(() => cloneSchema(today), [today]);
  const form = useForm<CloneInput, unknown, CloneInput>({
    resolver: zodResolver(schema),
    defaultValues: cloneDefaults(view === 'week' ? 'week' : 'day', shown, today),
  });
  const { errors } = form.formState;
  const values = useWatch({ control: form.control }) as CloneInput;

  // A preview or a result belongs to the values it was asked for: changing
  // the inputs discards it.
  const result = sameRequest(apply.variables, values) ? apply.data : undefined;
  const plan = !result && sameRequest(preview.variables, values) ? preview.data : undefined;
  const failed =
    (sameRequest(apply.variables, values) && apply.error) ||
    (sameRequest(preview.variables, values) && preview.error) ||
    null;

  const onServerError = (error: Error) => showServerErrors(error, form, cloneFields);
  const onSubmit = (request: CloneInput) => {
    apply.reset();
    preview.mutate(request, { onError: onServerError });
  };
  const onApply = () => {
    if (!preview.variables) return;
    apply.mutate(preview.variables, {
      onSuccess: (done) => toast.success(t('clone.done', { count: done.created.length })),
      onError: onServerError,
    });
  };

  const pickUnit = (unit: CloneUnit) => {
    const next = switchUnit(form.getValues(), unit, today);
    const options = { shouldValidate: form.formState.isSubmitted };
    form.setValue('unit', next.unit, options);
    form.setValue('source', next.source, options);
    form.setValue('target', next.target, options);
  };

  /** The day or week a date input names, as a hint. */
  const dateHint = (date: string) => {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return undefined;
    if (values.unit === 'day') return formatDayHeading(date, lang);
    if (weekdayOf(date) !== 1) return t('clone.mondayHint');
    return t('clone.weekRange', {
      from: formatDayHeading(date, lang),
      to: formatDayHeading(addDays(date, 6), lang),
    });
  };

  return (
    <>
      <FormSheetHeader title={t('clone.title')} description={t('clone.description')} />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={failed}
          messages={
            invalidFields(failed, cloneFields).length > 0 ? { 422: tAuth('form.checkEntries') } : {}
          }
        />
        <div className="grid gap-1.5">
          <Label id={unitLabel}>{t('clone.unit')}</Label>
          <Controller
            control={form.control}
            name="unit"
            render={({ field }) => (
              <ToggleGroup
                type="single"
                variant="outline"
                spacing={0}
                value={field.value}
                onValueChange={(value) => {
                  if (value === 'day' || value === 'week') pickUnit(value);
                }}
                aria-labelledby={unitLabel}
              >
                <ToggleGroupItem value="day">{t('clone.day')}</ToggleGroupItem>
                <ToggleGroupItem value="week">{t('clone.week')}</ToggleGroupItem>
              </ToggleGroup>
            )}
          />
        </div>
        <TextField
          label={t('clone.source')}
          type="date"
          hint={dateHint(values.source)}
          error={errorText(errors.source)}
          {...form.register('source')}
        />
        <TextField
          label={t('clone.target')}
          type="date"
          min={earliestTarget(values.unit, today)}
          hint={dateHint(values.target)}
          error={errorText(errors.target)}
          {...form.register('target')}
        />
        <div>
          <Button type="submit" variant="outline" disabled={preview.isPending}>
            <Eye aria-hidden="true" />
            {preview.isPending ? t('clone.previewing') : t('clone.preview')}
          </Button>
        </div>
      </form>

      {result ? (
        <CloneOutcome
          title={t('clone.resultTitle')}
          clone={result}
          timeZone={timeZone}
          createdLabel={(count) => t('clone.doneCreated', { count })}
          note={differs(preview.data, result) ? t('clone.changed') : undefined}
        >
          <SheetClose asChild>
            <Button type="button">{t('clone.close')}</Button>
          </SheetClose>
        </CloneOutcome>
      ) : plan ? (
        <CloneOutcome
          title={t('clone.previewTitle')}
          clone={plan}
          timeZone={timeZone}
          createdLabel={(count) => t('clone.created', { count })}
        >
          <Button
            type="button"
            disabled={plan.created.length === 0 || apply.isPending}
            onClick={onApply}
          >
            <Copy aria-hidden="true" />
            {apply.isPending
              ? t('clone.applying')
              : t('clone.apply', { count: plan.created.length })}
          </Button>
          <SheetClose asChild>
            <Button type="button" variant="outline">
              {t('form.cancel')}
            </Button>
          </SheetClose>
        </CloneOutcome>
      ) : null}
    </>
  );
}

/** A preview or the result of a clone: what is created and what is skipped, and why. */
function CloneOutcome({
  title,
  clone,
  timeZone,
  createdLabel,
  note,
  children,
}: {
  title: string;
  clone: LessonClone;
  timeZone: string;
  createdLabel: (count: number) => string;
  note?: string;
  children: ReactNode;
}) {
  const { t } = useTranslation('admin');
  const headingId = useId();
  const createdId = useId();
  const skippedId = useId();
  const { created, skipped } = clone;
  const empty = created.length === 0 && skipped.length === 0;

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 border-t px-4 pt-4 pb-6">
      <h3 id={headingId} className="font-heading text-base font-medium">
        {title}
      </h3>
      {note && <p className="text-sm text-muted-foreground">{note}</p>}
      {empty ? (
        <p className="text-sm">{t('clone.nothing')}</p>
      ) : (
        <>
          <div className="grid gap-2">
            <h4 id={createdId} className="text-sm font-medium">
              {createdLabel(created.length)}
            </h4>
            {created.length > 0 ? (
              <ul aria-labelledby={createdId} className="grid gap-2">
                {created.map((copy) => (
                  <CloneItem
                    key={copy.source.id}
                    lesson={copy.source}
                    startsAt={copy.startsAt}
                    endsAt={copy.endsAt}
                    timeZone={timeZone}
                  />
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">{t('clone.nothingToCreate')}</p>
            )}
          </div>
          {skipped.length > 0 && (
            <div className="grid gap-2">
              <h4 id={skippedId} className="text-sm font-medium">
                {t('clone.skipped', { count: skipped.length })}
              </h4>
              <ul aria-labelledby={skippedId} className="grid gap-2">
                {skipped.map((skip) => (
                  <CloneItem
                    key={skip.source.id}
                    lesson={skip.source}
                    startsAt={skip.startsAt}
                    endsAt={skip.endsAt}
                    timeZone={timeZone}
                  >
                    <SkipReason skip={skip} clone={clone} timeZone={timeZone} />
                  </CloneItem>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
      <div className="flex flex-wrap gap-2">{children}</div>
    </section>
  );
}

/**
 * A copy, made or not: its course type, school-local day and time, room
 * and staff, and the day of the lesson it copies.
 */
function CloneItem({
  lesson,
  startsAt,
  endsAt,
  timeZone,
  children,
}: {
  lesson: LessonSummary;
  startsAt: string;
  endsAt: string;
  timeZone: string;
  children?: ReactNode;
}) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  // A room rental names its renter instead of staff.
  const staff = [lesson.instructor?.displayName ?? lesson.renter, lesson.accompanist?.displayName]
    .filter(Boolean)
    .join(', ');
  return (
    <li className="grid gap-0.5 rounded-md border px-3 py-2 text-sm break-words">
      <span className="font-medium">{lesson.courseType.name}</span>
      <span>
        {formatDayHeading(schoolDateOf(startsAt, timeZone), lang)},{' '}
        <span className="tabular-nums">{formatTimeRange(startsAt, endsAt, timeZone, lang)}</span>
      </span>
      <span className="text-muted-foreground">
        {lesson.location.name} · {lesson.room.name} · {staff}
      </span>
      <span className="text-xs text-muted-foreground">
        {t('clone.from', {
          day: formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang),
        })}
      </span>
      {children}
    </li>
  );
}

/** Why a source lesson is not copied, naming the lesson in its room. */
function SkipReason({
  skip,
  clone,
  timeZone,
}: {
  skip: LessonClone['skipped'][number];
  clone: LessonClone;
  timeZone: string;
}) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const label = (courseType: string, startsAt: string, endsAt: string) =>
    t('lessons.label', {
      courseType,
      day: formatDayHeading(schoolDateOf(startsAt, timeZone), lang),
      time: formatTimeRange(startsAt, endsAt, timeZone, lang),
    });

  const roomTaken = (): string => {
    const room = skip.source.room.name;
    const taker = skip.lesson;
    if (!taker) return t('clone.reasons.roomTakenUnknown', { room });
    if (taker.copyOf) {
      // Another copy of this clone: name the lesson it copies.
      const original = clone.created.find((c) => c.source.id === taker.copyOf)?.source;
      return original
        ? t('clone.reasons.roomTakenByCopy', {
            room,
            lesson: label(original.courseType.name, original.startsAt, original.endsAt),
          })
        : t('clone.reasons.roomTakenByAnyCopy', { room });
    }
    // Any other lesson, with or without an id (a class lesson not generated
    // yet has none): name its course type and time.
    return t('clone.reasons.roomTaken', {
      room,
      lesson: label(taker.courseType.name, taker.startsAt, taker.endsAt),
    });
  };

  let reason: string;
  switch (skip.reason) {
    case 'class-slot':
      reason = t('clone.reasons.classSlot');
      break;
    case 'started':
      reason = t('clone.reasons.started');
      break;
    case 'staff-inactive':
      reason = t('clone.reasons.staffInactive');
      break;
    case 'room-taken':
      reason = roomTaken();
      break;
    default: {
      // tsc fails here when the API gains a reason without a text; at run
      // time an unknown one shows as its code rather than breaking the sheet.
      const unknown: never = skip.reason;
      reason = String(unknown);
    }
  }
  return <span className="mt-1 font-medium">{reason}</span>;
}
