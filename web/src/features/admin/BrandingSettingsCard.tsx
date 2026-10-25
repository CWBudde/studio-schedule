import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Save, Undo2 } from 'lucide-react';
import { useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type BrandingSettings,
  type BrandingSettingsData,
  brandingSettingsQuery,
  updateBrandingSettingsMutation,
} from '@/api/queries';
import { useSchoolLogoUrl } from '@/components/layout/branding';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { TextField } from '@/features/auth/TextField';
import { accentContrast, darkAccent, minAccentContrast, parseColor, toHex } from '@/lib/color';
import { showServerErrors } from './crud/form';
import { Section } from './PersonSection';
import {
  brandingFields,
  brandingSchema,
  type BrandingValues,
  brandingValues,
} from './settingsForms';
import { useSettingsErrorText } from './settingsErrors';
import { SettingsFacts, SettingsLoaded } from './settingsShared';

// The theme's colors around the primary one (web/src/index.css), for the preview.
const light = { page: 'oklch(0.978 0.008 75)', text: 'oklch(0.985 0.008 75)' };
const dark = { page: 'oklch(0.17 0.02 340)', text: 'oklch(0.2 0.03 345)' };

/**
 * "Branding": the accent color, checked for contrast while it is typed,
 * with a preview of it in the light and the dark theme; and the logo,
 * which is the receipts' logo. An admin changes the color; the office sees
 * it.
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

  const onSubmit = (body: BrandingSettingsData) =>
    update.mutate(body, {
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
