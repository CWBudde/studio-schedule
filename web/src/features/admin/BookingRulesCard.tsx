import { zodResolver } from '@hookform/resolvers/zod';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Save, XIcon } from 'lucide-react';
import { type ReactNode, useMemo } from 'react';
import { Controller, type UseFormRegisterReturn, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { invalidFields } from '@/features/auth/form';
import {
  type BookingRules,
  type BookingRulesData,
  bookingRulesQuery,
  publicSettingsQuery,
  releasePreviewQuery,
  type ReleaseWindow,
  updateBookingRulesMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { TextField } from '@/features/auth/TextField';
import {
  addDays,
  formatDate,
  formatDateTime,
  formatDayHeading,
  schoolDateOf,
} from '@/lib/schoolTime';
import { SelectField } from './crud/fields';
import { useDebouncedValue } from './crud/useDebouncedValue';
import { useDescription } from './crud/useDescription';
import { Section } from './PersonSection';
import {
  durations,
  isSchoolDate,
  maxReleasedThroughDays,
  type RulesField,
  rulesSchemaUntil,
  rulesServerFields,
  type RulesValues,
  rulesValues,
  splitMinutes,
  type Unit,
  units,
} from './settingsForms';
import { useSettingsErrorText } from './settingsErrors';
import { SettingsFacts, SettingsLoaded } from './settingsShared';

/** A Sunday: the school date of weekday 0, to name the weekdays. */
const aSunday = '2026-10-25';

function useWeekdayName() {
  const { i18n } = useTranslation();
  return (weekday: number) =>
    formatDate(addDays(aSunday, weekday), i18n.language, { weekday: 'long' });
}

/**
 * "Booking rules": the weekly release (day, time, weeks ahead), until when
 * members book and cancel, how long a waiting-list invitation holds its
 * place, and how far ahead members see. The release window shows what the
 * rules release now, computed by the server for what is typed (ADR 0003).
 * An admin changes them; a wider window creates its lessons at once. The
 * office reads them.
 */
export function BookingRulesCard({ admin }: { admin: boolean }) {
  const { t } = useTranslation('settings');
  const query = useQuery(bookingRulesQuery);
  return (
    <Section title={t('rules.title')} description={t('rules.description')}>
      <SettingsLoaded query={query}>
        {(rules) => (admin ? <RulesForm rules={rules} /> : <RulesFacts rules={rules} />)}
      </SettingsLoaded>
    </Section>
  );
}

function useDuration() {
  const { t } = useTranslation('settings');
  return (minutes: number) => {
    const { amount, unit } = splitMinutes(minutes);
    return t(`rules.duration.${unit}`, { count: Number(amount) });
  };
}

function RulesFacts({ rules }: { rules: BookingRules }) {
  const { t, i18n } = useTranslation('settings');
  const weekday = useWeekdayName();
  const duration = useDuration();
  return (
    <SettingsFacts
      rows={[
        [t('rules.releaseWeekday'), weekday(rules.releaseWeekday)],
        [t('rules.releaseTime'), rules.releaseTime],
        [t('rules.releaseWeeksAhead'), String(rules.releaseWeeksAhead)],
        [
          t('rules.manualReleasedThrough'),
          rules.releasedThrough ? formatDayHeading(rules.releasedThrough, i18n.language) : '',
        ],
        [t('rules.bookingCutoff'), duration(rules.bookingCutoffMinutes)],
        [t('rules.cancellationCutoff'), duration(rules.cancellationCutoffMinutes)],
        [t('rules.waitlistInviteExpiry'), duration(rules.waitlistInviteExpiryMinutes)],
        [t('rules.defaultVisibleDays'), String(rules.defaultVisibleDays)],
      ]}
    >
      <Window window={rules.window} />
    </SettingsFacts>
  );
}

/** The last school date before an instant of the window (a school-local midnight). */
const lastDayBefore = (instant: string, zone: string) =>
  schoolDateOf(Date.parse(instant) - 1, zone);

/** Whether the released-through date reaches beyond the weekly release (the server decides). */
const extended = (w: ReleaseWindow) =>
  Date.parse(w.releasedUntil) > Date.parse(w.ruleReleasedUntil);

/**
 * The release window: the last and the next release, and the last day
 * released; when the released-through date reaches further, also the last
 * day of the weekly release alone. Without one, a rule being typed is
 * incomplete or could not be previewed, or the window is still loading.
 */
function Window({
  window: w,
  missing = 'loading',
  hint,
}: {
  window: ReleaseWindow | undefined;
  missing?: PreviewMissing;
  hint?: string;
}) {
  const { t, i18n } = useTranslation('settings');
  const zone = useQuery(publicSettingsQuery).data?.timezone;
  return (
    <div className="space-y-1 rounded-md border bg-muted/40 p-3 text-sm" aria-live="polite">
      <h3 className="font-medium">{t('rules.window')}</h3>
      {w && zone ? (
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5">
          <dt className="text-muted-foreground">{t('rules.lastRelease')}</dt>
          <dd>{formatDateTime(Date.parse(w.lastReleaseAt), zone, i18n.language)}</dd>
          <dt className="text-muted-foreground">{t('rules.nextRelease')}</dt>
          <dd>{formatDateTime(Date.parse(w.nextReleaseAt), zone, i18n.language)}</dd>
          <dt className="text-muted-foreground">{t('rules.releasedThrough')}</dt>
          <dd>
            {formatDayHeading(
              lastDayBefore(w.releasedUntil, zone),
              i18n.language,
              schoolDateOf(Date.parse(w.now), zone),
            )}
          </dd>
          {extended(w) && (
            <>
              <dt className="text-muted-foreground">{t('rules.ruleReleasedThrough')}</dt>
              <dd>
                {formatDayHeading(
                  lastDayBefore(w.ruleReleasedUntil, zone),
                  i18n.language,
                  schoolDateOf(Date.parse(w.now), zone),
                )}
              </dd>
            </>
          )}
        </dl>
      ) : missing === 'loading' ? (
        <p role="status" className="text-muted-foreground">
          {t('rules.previewLoading')}
        </p>
      ) : (
        <p className="text-muted-foreground">
          {missing === 'incomplete' ? t('rules.previewIncomplete') : t('rules.previewFailed')}
        </p>
      )}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

const releaseTimePattern = /^([01]\d|2[0-3]):[0-5]\d$/;

/** Why a preview shows no window: it loads, the rule is incomplete, or it failed. */
type PreviewMissing = 'loading' | 'incomplete' | 'failed';

/** The window of the release rule being typed, once it is valid and settled. */
function usePreview(
  values: Pick<
    RulesValues,
    'releaseWeekday' | 'releaseTime' | 'releaseWeeksAhead' | 'releasedThrough'
  >,
  latest: string | undefined,
): { window?: ReleaseWindow; missing: PreviewMissing; settled: boolean } {
  const weeks = Number(values.releaseWeeksAhead);
  const through = values.releasedThrough.trim();
  const valid =
    releaseTimePattern.test(values.releaseTime) &&
    Number.isInteger(weeks) &&
    weeks >= 1 &&
    weeks <= 52 &&
    (through === '' || (isSchoolDate(through) && (!latest || through <= latest)));
  const params = useDebouncedValue(
    valid
      ? {
          releaseWeekday: Number(values.releaseWeekday),
          releaseTime: values.releaseTime,
          releaseWeeksAhead: weeks,
          ...(through ? { releasedThrough: through } : {}),
        }
      : undefined,
    300,
  );
  const preview = useQuery({
    ...releasePreviewQuery(
      params ?? { releaseWeekday: 0, releaseTime: '00:00', releaseWeeksAhead: 1 },
    ),
    enabled: params !== undefined,
    placeholderData: keepPreviousData,
  });
  // The window answers the typed date only once the debounce and the query
  // have caught up; until then it may still be the previous date's.
  const settled = !preview.isPlaceholderData && (params?.releasedThrough ?? '') === through;
  if (!valid) return { missing: 'incomplete', settled: false };
  if (!params) return { missing: 'loading', settled: false }; // still being typed
  if (preview.isError) return { missing: 'failed', settled: false };
  return { window: preview.data, missing: 'loading', settled };
}

function RulesForm({ rules }: { rules: BookingRules }) {
  const { t } = useTranslation('settings');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useSettingsErrorText();
  const weekday = useWeekdayName();
  const queryClient = useQueryClient();
  const update = useMutation(updateBookingRulesMutation(queryClient));
  const zone = useQuery(publicSettingsQuery).data?.timezone;
  // The latest released-through date: 52 weeks after the server's today
  // in the school's zone; the server checks it too.
  const latest = zone
    ? addDays(schoolDateOf(Date.parse(rules.window.now), zone), maxReleasedThroughDays)
    : undefined;
  const resolver = useMemo(() => zodResolver(rulesSchemaUntil(latest)), [latest]);
  const form = useForm<RulesValues, unknown, BookingRulesData>({
    resolver,
    defaultValues: rulesValues(rules),
  });
  const { errors } = form.formState;
  const [releaseWeekday, releaseTime, releaseWeeksAhead, releasedThrough] = useWatch({
    control: form.control,
    name: ['releaseWeekday', 'releaseTime', 'releaseWeeksAhead', 'releasedThrough'],
  });
  const preview = usePreview(
    { releaseWeekday, releaseTime, releaseWeeksAhead, releasedThrough },
    latest,
  );

  const onSubmit = (body: BookingRulesData) =>
    update.mutate(body, {
      onSuccess: (saved) => {
        form.reset(rulesValues(saved));
        toast.success(
          saved.generatedLessons > 0
            ? t('rules.generated', { count: saved.generatedLessons })
            : t('saved'),
        );
      },
      onError: (error) => {
        const api = Object.keys(rulesServerFields) as (keyof typeof rulesServerFields)[];
        for (const field of invalidFields(error, api)) {
          const formField = rulesServerFields[field];
          if (formField) {
            form.setError(formField, {
              type: 'server',
              // The format is checked here; the server refuses a date too far ahead.
              message: formField === 'releasedThrough' ? 'releasedThroughLatest' : 'invalid',
            });
          }
        }
      },
    });

  const duration = (d: (typeof durations)[number]) => (
    <DurationField
      key={d.field}
      label={t(`rules.${d.field}`)}
      hint={t(`rules.${d.field}Hint`)}
      error={errorText(errors[d.field])}
      amount={form.register(d.field)}
      unit={
        <Controller
          control={form.control}
          name={`${d.field}Unit` as RulesField}
          render={({ field }) => (
            <UnitSelect
              label={t('rules.unit', { field: t(`rules.${d.field}`) })}
              value={field.value as Unit}
              onChange={field.onChange}
            />
          )}
        />
      }
    />
  );

  return (
    <form
      noValidate
      aria-label={t('rules.title')}
      className="space-y-4"
      onSubmit={form.handleSubmit(onSubmit)}
    >
      <ProblemAlert error={update.error} messages={{ 422: tAuth('form.checkEntries') }} />
      <div className="grid gap-4 sm:grid-cols-2">
        <Controller
          control={form.control}
          name="releaseWeekday"
          render={({ field }) => (
            <SelectField
              label={t('rules.releaseWeekday')}
              placeholder={t('rules.releaseWeekday')}
              options={[0, 1, 2, 3, 4, 5, 6].map((d) => ({ value: String(d), label: weekday(d) }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.releaseWeekday)}
            />
          )}
        />
        <TextField
          label={t('rules.releaseTime')}
          type="time"
          error={errorText(errors.releaseTime)}
          {...form.register('releaseTime')}
        />
      </div>
      <TextField
        label={t('rules.releaseWeeksAhead')}
        inputMode="numeric"
        hint={t('rules.releaseWeeksAheadHint')}
        error={errorText(errors.releaseWeeksAhead)}
        {...form.register('releaseWeeksAhead')}
      />
      <ReleasedThroughField
        label={t('rules.manualReleasedThrough')}
        clearLabel={t('rules.manualReleasedThroughClear')}
        hint={t('rules.manualReleasedThroughHint')}
        effect={
          preview.settled ? releaseEffect(preview.window, releasedThrough.trim() !== '') : undefined
        }
        error={errorText(errors.releasedThrough)}
        max={latest}
        input={form.register('releasedThrough')}
        onClear={() =>
          form.setValue('releasedThrough', '', { shouldDirty: true, shouldValidate: true })
        }
      />
      <Window window={preview.window} missing={preview.missing} hint={t('rules.previewHint')} />
      {durations.map(duration)}
      <TextField
        label={t('rules.defaultVisibleDays')}
        inputMode="numeric"
        hint={t('rules.defaultVisibleDaysHint')}
        error={errorText(errors.defaultVisibleDays)}
        {...form.register('defaultVisibleDays')}
      />
      <Button type="submit" variant="outline" disabled={update.isPending}>
        <Save aria-hidden="true" />
        {update.isPending ? tAdmin('form.saving') : tAdmin('form.save')}
      </Button>
    </form>
  );
}

/** What the released-through date does with the window being previewed. */
type ReleaseEffect = { kind: 'rule' | 'extends' | 'noEffect'; window: ReleaseWindow } | undefined;

function releaseEffect(w: ReleaseWindow | undefined, dated: boolean): ReleaseEffect {
  if (!w) return undefined;
  if (!dated) return { kind: 'rule', window: w };
  return { kind: extended(w) ? 'extends' : 'noEffect', window: w };
}

const effectKeys = {
  rule: 'rules.ruleReleasesThrough',
  extends: 'rules.manualExtends',
  noEffect: 'rules.manualNoEffect',
} as const;

/**
 * The optional released-through date: a date input with a button that
 * clears it, the hint, and what the date does now, so the office sees
 * whether it matters (the weekly release alone, from the server's preview).
 */
function ReleasedThroughField({
  label,
  clearLabel,
  hint,
  effect,
  error,
  max,
  input,
  onClear,
}: {
  label: string;
  clearLabel: string;
  hint: string;
  effect: ReleaseEffect;
  error: string | undefined;
  max: string | undefined;
  input: UseFormRegisterReturn;
  onClear: () => void;
}) {
  const { t, i18n } = useTranslation('settings');
  const zone = useQuery(publicSettingsQuery).data?.timezone;
  const effectText =
    effect && zone
      ? t(effectKeys[effect.kind], {
          date: formatDayHeading(
            lastDayBefore(effect.window.ruleReleasedUntil, zone),
            i18n.language,
            schoolDateOf(Date.parse(effect.window.now), zone),
          ),
        })
      : undefined;
  const { id, describedBy, lines } = useDescription(
    effectText ? `${hint} ${effectText}` : hint,
    error,
  );
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex gap-2">
        <Input
          id={id}
          type="date"
          className="w-44"
          max={max}
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy}
          {...input}
        />
        <Button type="button" variant="ghost" size="icon" aria-label={clearLabel} onClick={onClear}>
          <XIcon aria-hidden="true" />
        </Button>
      </div>
      {lines}
    </div>
  );
}

/** A duration: a whole number and its unit. */
function DurationField({
  label,
  hint,
  error,
  amount,
  unit,
}: {
  label: string;
  hint: string;
  error: string | undefined;
  amount: UseFormRegisterReturn;
  unit: ReactNode;
}) {
  const { id, describedBy, lines } = useDescription(hint, error);
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex gap-2">
        <Input
          id={id}
          inputMode="numeric"
          className="w-28"
          aria-invalid={error ? true : undefined}
          aria-describedby={describedBy}
          {...amount}
        />
        {unit}
      </div>
      {lines}
    </div>
  );
}

function UnitSelect({
  label,
  value,
  onChange,
}: {
  label: string;
  value: Unit;
  onChange: (unit: Unit) => void;
}) {
  const { t } = useTranslation('settings');
  return (
    <Select value={value} onValueChange={(v) => onChange(v as Unit)}>
      <SelectTrigger aria-label={label} className="w-36">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {units.map((u) => (
          <SelectItem key={u} value={u}>
            {t(`rules.units.${u}`)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
