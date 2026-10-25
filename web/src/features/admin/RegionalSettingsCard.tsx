import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Save } from 'lucide-react';
import { useId, useMemo } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type RegionalSettings,
  type RegionalSettingsData,
  regionalSettingsQuery,
  updateRegionalSettingsMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { TextField } from '@/features/auth/TextField';
import { SelectField } from './crud/fields';
import { showServerErrors } from './crud/form';
import { Section } from './PersonSection';
import {
  locales,
  regionalFields,
  regionalSchema,
  type RegionalValues,
  regionalValues,
} from './settingsForms';
import { useSettingsErrorText } from './settingsErrors';
import { SettingsFacts, SettingsLoaded } from './settingsShared';

/**
 * "Time and language": the school's time zone, its default language and
 * its currency. The language may always change; the time zone only until
 * the first lessons exist, the currency until the first price or receipt
 * (ADR 0013): the card says why they are fixed. An admin changes them; the
 * office reads them.
 */
export function RegionalSettingsCard({ admin }: { admin: boolean }) {
  const { t } = useTranslation('settings');
  const query = useQuery(regionalSettingsQuery);
  return (
    <Section title={t('regional.title')} description={t('regional.description')}>
      <SettingsLoaded query={query}>
        {(settings) =>
          admin ? (
            <RegionalForm key={JSON.stringify(settings)} settings={settings} />
          ) : (
            <SettingsFacts
              rows={[
                [t('regional.timezone'), settings.timezone],
                [t('regional.locale'), t(`regional.locales.${settings.defaultLocale}`)],
                [t('regional.currency'), settings.currency],
              ]}
            >
              {settings.timezoneLocked && (
                <p className="text-muted-foreground">{t('regional.timezoneLocked')}</p>
              )}
              {settings.currencyLocked && (
                <p className="text-muted-foreground">{t('regional.currencyLocked')}</p>
              )}
            </SettingsFacts>
          )
        }
      </SettingsLoaded>
    </Section>
  );
}

/** The browser's IANA time zones, for suggestions; none where it cannot list them. */
function useTimeZones(): string[] {
  return useMemo(() => {
    try {
      return Intl.supportedValuesOf('timeZone');
    } catch {
      return [];
    }
  }, []);
}

function RegionalForm({ settings }: { settings: RegionalSettings }) {
  const { t } = useTranslation('settings');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useSettingsErrorText();
  const queryClient = useQueryClient();
  const update = useMutation(updateRegionalSettingsMutation(queryClient));
  const zonesId = useId();
  const zones = useTimeZones();
  const form = useForm<RegionalValues, unknown, RegionalSettingsData>({
    resolver: zodResolver(regionalSchema),
    defaultValues: regionalValues(settings),
  });
  const { errors } = form.formState;

  const onSubmit = (body: RegionalSettingsData) =>
    update.mutate(body, {
      onSuccess: () => toast.success(t('saved')),
      onError: (error) => showServerErrors(error, form, regionalFields),
    });

  return (
    <form
      noValidate
      aria-label={t('regional.title')}
      className="space-y-4"
      onSubmit={form.handleSubmit(onSubmit)}
    >
      <ProblemAlert
        error={update.error}
        messages={{ 422: tAuth('form.checkEntries') }}
        conflicts={{
          'timezone-locked': t('regional.conflicts.timezone-locked'),
          'currency-locked': t('regional.conflicts.currency-locked'),
        }}
      />
      <TextField
        label={t('regional.timezone')}
        autoComplete="off"
        list={zones.length > 0 ? zonesId : undefined}
        readOnly={settings.timezoneLocked}
        hint={settings.timezoneLocked ? t('regional.timezoneLocked') : t('regional.timezoneHint')}
        error={errorText(errors.timezone)}
        {...form.register('timezone')}
      />
      {zones.length > 0 && !settings.timezoneLocked && (
        <datalist id={zonesId}>
          {zones.map((zone) => (
            <option key={zone} value={zone} />
          ))}
        </datalist>
      )}
      <Controller
        control={form.control}
        name="defaultLocale"
        render={({ field }) => (
          <SelectField
            label={t('regional.locale')}
            placeholder={t('regional.locale')}
            hint={t('regional.localeHint')}
            options={locales.map((l) => ({ value: l, label: t(`regional.locales.${l}`) }))}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.defaultLocale)}
          />
        )}
      />
      <TextField
        label={t('regional.currency')}
        autoComplete="off"
        maxLength={3}
        readOnly={settings.currencyLocked}
        className="uppercase"
        hint={settings.currencyLocked ? t('regional.currencyLocked') : t('regional.currencyHint')}
        error={errorText(errors.currency)}
        {...form.register('currency')}
      />
      <Button type="submit" variant="outline" disabled={update.isPending}>
        <Save aria-hidden="true" />
        {update.isPending ? tAdmin('form.saving') : tAdmin('form.save')}
      </Button>
    </form>
  );
}
