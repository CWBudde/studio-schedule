import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ImageUp, Save, Trash2, Undo2 } from 'lucide-react';
import { type ChangeEvent, useId, useState } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type BrandingSettings,
  type BrandingSettingsData,
  brandingSettingsQuery,
  deleteBrandingBackgroundMutation,
  schoolBackgroundQuery,
  updateBrandingBackgroundMutation,
  updateBrandingSettingsMutation,
} from '@/api/queries';
import { useSchoolLogoUrl } from '@/components/layout/branding';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { FormError } from '@/features/auth/FormError';
import { invalidFields } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import { accentContrast, darkAccent, minAccentContrast, parseColor, toHex } from '@/lib/color';
import { useDataUrl } from '@/lib/useDataUrl';
import { SelectField } from './crud/fields';
import { showServerErrors } from './crud/form';
import { Section } from './PersonSection';
import { fileBase64 } from './receiptSettingsForms';
import {
  brandingFields,
  brandingSchema,
  type BrandingValues,
  brandingValues,
  themes,
} from './settingsForms';
import { useSettingsErrorText } from './settingsErrors';
import { SettingsFacts, SettingsLoaded } from './settingsShared';

// The theme's colors around the primary one (web/src/index.css), for the preview.
const light = { page: 'oklch(0.978 0.008 75)', text: 'oklch(0.985 0.008 75)' };
const dark = { page: 'oklch(0.17 0.02 340)', text: 'oklch(0.2 0.03 345)' };

/** The background image as the API takes it (domain.MaxBackgroundBytes and its types). */
const maxBackgroundBytes = 1 << 20;
const backgroundTypes = ['image/jpeg', 'image/png', 'image/webp'] as const;
type BackgroundType = (typeof backgroundTypes)[number];
const isBackgroundType = (type: string): type is BackgroundType =>
  backgroundTypes.includes(type as BackgroundType);

/**
 * "Branding": the theme; the accent color, checked for contrast while it
 * is typed, with a preview of it in the light and the dark theme; the
 * logo, which is the receipts' logo; and the background image. An admin
 * changes them; the office sees them.
 */
export function BrandingSettingsCard({ admin }: { admin: boolean }) {
  const { t } = useTranslation('settings');
  const query = useQuery(brandingSettingsQuery);
  return (
    <Section title={t('branding.title')} description={t('branding.description')}>
      <SettingsLoaded query={query}>
        {(settings) => (
          <>
            {admin ? (
              <BrandingForm settings={settings} />
            ) : (
              <SettingsFacts
                rows={[
                  [t('branding.theme'), t(`branding.themes.${settings.theme}`)],
                  [
                    t('branding.accent'),
                    settings.accentColor ? (
                      <Swatch color={settings.accentColor} />
                    ) : (
                      t('branding.own')
                    ),
                  ],
                ]}
              />
            )}
            <Logo hasLogo={settings.hasLogo} />
            <Background hasBackground={settings.hasBackground} admin={admin} />
          </>
        )}
      </SettingsLoaded>
    </Section>
  );
}

function Swatch({ color }: { color: string }) {
  return (
    <span className="inline-flex items-center gap-2">
      <span
        aria-hidden="true"
        className="inline-block size-4 rounded-sm border"
        style={{ backgroundColor: color }}
      />
      <span className="font-mono">{color}</span>
    </span>
  );
}

function BrandingForm({ settings }: { settings: BrandingSettings }) {
  const { t, i18n } = useTranslation('settings');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useSettingsErrorText();
  const queryClient = useQueryClient();
  const update = useMutation(updateBrandingSettingsMutation(queryClient));
  const form = useForm<BrandingValues, unknown, BrandingSettingsData>({
    resolver: zodResolver(brandingSchema),
    defaultValues: brandingValues(settings),
  });
  const { errors } = form.formState;
  const typed = useWatch({ control: form.control, name: 'accentColor' }).trim();
  const color = parseColor(typed);
  const contrast = color && accentContrast(color);
  const ratio = (n: number) =>
    new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 2 }).format(n);

  // An unchanged theme is left out, so that the stored one stays. "Unchanged" is
  // against the form's own baseline: a refetch (e.g. after a background upload)
  // may bring another admin's theme into settings while the form keeps its own.
  const onSubmit = ({ theme, ...body }: BrandingSettingsData) =>
    update.mutate(theme === form.formState.defaultValues?.theme ? body : { ...body, theme }, {
      onSuccess: (saved) => {
        form.reset(brandingValues(saved));
        toast.success(t('saved'));
      },
      onError: (error) => showServerErrors(error, form, brandingFields),
    });

  const set = (value: string) =>
    form.setValue('accentColor', value, { shouldDirty: true, shouldValidate: true });

  return (
    <form
      noValidate
      aria-label={t('branding.title')}
      className="space-y-4"
      onSubmit={form.handleSubmit(onSubmit)}
    >
      <ProblemAlert error={update.error} messages={{ 422: tAuth('form.checkEntries') }} />
      <Controller
        control={form.control}
        name="theme"
        render={({ field }) => (
          <SelectField
            label={t('branding.theme')}
            placeholder={t('branding.chooseTheme')}
            hint={t(`branding.themeHints.${field.value}`)}
            options={themes.map((id) => ({ value: id, label: t(`branding.themes.${id}`) }))}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.theme)}
          />
        )}
      />
      <TextField
        label={t('branding.accent')}
        autoComplete="off"
        placeholder="#7a1f3d"
        maxLength={7}
        className="font-mono"
        hint={
          contrast === undefined
            ? t('branding.accentHint')
            : `${t('branding.contrast', { ratio: ratio(contrast) })} – ${
                contrast >= minAccentContrast ? t('branding.contrastOk') : t('branding.contrastLow')
              }`
        }
        error={errorText(errors.accentColor)}
        {...form.register('accentColor')}
      />
      <label className="flex w-fit cursor-pointer items-center gap-2 text-sm">
        <input
          type="color"
          value={color ? toHex(color) : '#7a1f3d'}
          onChange={(e) => set(e.target.value)}
          className="h-8 w-10 cursor-pointer rounded-md border bg-transparent p-0.5"
        />
        {t('branding.pick')}
      </label>
      {color && (
        <div className="space-y-1.5">
          <p className="text-sm font-medium">{t('branding.preview')}</p>
          <div className="grid grid-cols-2 gap-2">
            <Preview
              label={t('branding.previewLight')}
              page={light.page}
              primary={toHex(color)}
              text={light.text}
            />
            <Preview
              label={t('branding.previewDark')}
              page={dark.page}
              primary={toHex(darkAccent(color))}
              text={dark.text}
              dark
            />
          </div>
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="outline" disabled={update.isPending}>
          <Save aria-hidden="true" />
          {update.isPending ? tAdmin('form.saving') : tAdmin('form.save')}
        </Button>
        {typed !== '' && (
          <Button type="button" variant="ghost" onClick={() => set('')}>
            <Undo2 aria-hidden="true" />
            {t('branding.useOwn')}
          </Button>
        )}
      </div>
    </form>
  );
}

/** The accent on a theme's page: a button with its text, as the app draws it. */
function Preview({
  label,
  page,
  primary,
  text,
  dark = false,
}: {
  label: string;
  page: string;
  primary: string;
  text: string;
  dark?: boolean;
}) {
  const { t } = useTranslation('settings');
  return (
    <div
      className="flex items-center justify-between gap-2 rounded-md border p-3"
      style={{ backgroundColor: page }}
    >
      <span
        className="text-xs font-medium"
        style={{ color: dark ? 'oklch(0.94 0.012 75)' : 'oklch(0.22 0.025 345)' }}
      >
        {label}
      </span>
      <span
        aria-hidden="true"
        className="rounded-md px-3 py-1 text-sm font-medium"
        style={{ backgroundColor: primary, color: text }}
      >
        {t('branding.sample')}
      </span>
    </div>
  );
}

/** The logo the header shows: the receipts' logo, changed with the receipt settings. */
function Logo({ hasLogo }: { hasLogo: boolean }) {
  const { t } = useTranslation('settings');
  const url = useSchoolLogoUrl(hasLogo);
  return (
    <div className="space-y-2 border-t pt-4">
      <h3 className="text-sm font-medium">{t('branding.logo')}</h3>
      {hasLogo && url && (
        <img
          src={url}
          alt={t('branding.logoAlt')}
          className="max-h-16 max-w-full rounded border bg-white object-contain p-2"
        />
      )}
      <p className="text-sm text-muted-foreground">
        {hasLogo ? t('branding.logoHint') : t('branding.noLogo')}
      </p>
    </div>
  );
}

/**
 * The school's background image, which themes that show one put behind the
 * app: shown, and for an admin uploaded, replaced or removed.
 */
function Background({ hasBackground, admin }: { hasBackground: boolean; admin: boolean }) {
  const { t } = useTranslation('settings');
  const queryClient = useQueryClient();
  const inputId = useId();
  const hintId = useId();
  const errorId = useId();
  const image = useQuery({ ...schoolBackgroundQuery, enabled: hasBackground });
  const upload = useMutation(updateBrandingBackgroundMutation(queryClient));
  const remove = useMutation(deleteBrandingBackgroundMutation(queryClient));
  /** Why the chosen file was not sent: not a JPEG, PNG or WebP or too large, or unreadable. */
  const [refused, setRefused] = useState<'invalid' | 'unreadable'>();
  const url = useDataUrl(hasBackground ? image.data : null);
  const pending = upload.isPending || remove.isPending;
  const megabytes = maxBackgroundBytes / (1 << 20);
  // The server's 422 names the type or the data: the same rule as checked here.
  const problem =
    refused ??
    (invalidFields(upload.error, ['contentType', 'data']).length ? 'invalid' : undefined);

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    // Only the answer of this attempt is shown.
    upload.reset();
    remove.reset();
    const type = file.type;
    if (file.size > maxBackgroundBytes || !isBackgroundType(type)) {
      setRefused('invalid');
      return;
    }
    let data: string;
    try {
      data = await fileBase64(file);
    } catch {
      setRefused('unreadable');
      return;
    }
    setRefused(undefined);
    upload.mutate(
      { contentType: type, data },
      { onSuccess: () => toast.success(t('branding.background.saved')) },
    );
  };

  const onRemove = () => {
    upload.reset();
    setRefused(undefined);
    remove.mutate(undefined, { onSuccess: () => toast.success(t('branding.background.removed')) });
  };

  return (
    <div className="space-y-3 border-t pt-4">
      <h3 className="text-sm font-medium">{t('branding.background.title')}</h3>
      {url ? (
        <img
          src={url}
          alt={t('branding.background.alt')}
          className="max-h-40 max-w-full rounded border object-cover"
        />
      ) : (
        <p className="text-sm text-muted-foreground">
          {hasBackground ? t('branding.background.loading') : t('branding.background.none')}
        </p>
      )}
      {admin && (
        <>
          {problem && (
            <p id={errorId} role="alert" className="text-sm text-destructive">
              {t(`branding.background.${problem}`, { size: megabytes })}
            </p>
          )}
          {!problem && <FormError error={upload.error ?? remove.error} />}
          <div className="flex flex-wrap items-center gap-2">
            <input
              id={inputId}
              type="file"
              accept={backgroundTypes.join(',')}
              className="peer sr-only"
              disabled={pending}
              aria-invalid={problem ? true : undefined}
              aria-describedby={problem ? `${errorId} ${hintId}` : hintId}
              onChange={(e) => void onFile(e)}
            />
            <Button asChild variant="outline" size="sm" disabled={pending}>
              <label
                htmlFor={inputId}
                className="cursor-pointer peer-focus-visible:ring-[3px] peer-focus-visible:ring-ring/50"
              >
                <ImageUp aria-hidden="true" />
                {upload.isPending
                  ? t('branding.background.uploading')
                  : hasBackground
                    ? t('branding.background.replace')
                    : t('branding.background.upload')}
              </label>
            </Button>
            {hasBackground && (
              <Button variant="outline" size="sm" disabled={pending} onClick={onRemove}>
                <Trash2 aria-hidden="true" />
                {t('branding.background.remove')}
              </Button>
            )}
          </div>
          <p id={hintId} className="text-xs text-muted-foreground">
            {t('branding.background.hint', { size: megabytes })}
          </p>
        </>
      )}
    </div>
  );
}
