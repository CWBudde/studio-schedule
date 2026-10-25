import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Save } from 'lucide-react';
import { type FormEvent, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { passSettingsQuery, type RedemptionMode, updatePassSettingsMutation } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { FormError } from '@/features/auth/FormError';
import { SelectField } from './crud/fields';
import { LoadError } from './crud/LoadError';
import { Section } from './PersonSection';

const modes = ['attendance', 'booking'] as const satisfies RedemptionMode[];

/**
 * "Passes": when bookings use a pass, on booking or on attendance. An admin
 * changes it (from now on; what was used stays used); the office sees it.
 */
export function PassSettingsCard({ admin }: { admin: boolean }) {
  const { t } = useTranslation('passes');
  const settings = useQuery(passSettingsQuery);
  return (
    <Section title={t('settings.title')} description={t('settings.description')}>
      {settings.data ? (
        admin ? (
          <ModeForm key={settings.data.redemptionMode} current={settings.data.redemptionMode} />
        ) : (
          <div className="space-y-2 text-sm">
            <p>
              <span className="text-muted-foreground">{t('settings.mode')}: </span>
              <span className="font-medium">
                {t(`settings.modes.${settings.data.redemptionMode}`)}
              </span>
            </p>
            <p className="text-muted-foreground">
              {t(`settings.modeHints.${settings.data.redemptionMode}`)}
            </p>
            <p className="text-muted-foreground">{t('settings.adminOnly')}</p>
          </div>
        )
      ) : settings.isError ? (
        <LoadError message={t('settings.loadError')} onRetry={() => void settings.refetch()} />
      ) : (
        <p role="status" className="text-sm text-muted-foreground">
          {t('settings.loading')}
        </p>
      )}
    </Section>
  );
}

function ModeForm({ current }: { current: RedemptionMode }) {
  const { t } = useTranslation('passes');
  const { t: tAdmin } = useTranslation('admin');
  const queryClient = useQueryClient();
  const update = useMutation(updatePassSettingsMutation(queryClient));
  const [mode, setMode] = useState<RedemptionMode>(current);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    update.mutate(mode, { onSuccess: () => toast.success(t('settings.saved')) });
  };

  return (
    <form noValidate aria-label={t('settings.title')} className="space-y-4" onSubmit={onSubmit}>
      <FormError error={update.error} />
      <SelectField
        label={t('settings.mode')}
        placeholder={t('settings.mode')}
        hint={`${t(`settings.modeHints.${mode}`)} ${t('settings.notRetroactive')}`}
        options={modes.map((m) => ({ value: m, label: t(`settings.modes.${m}`) }))}
        value={mode}
        onChange={(value) => setMode(value as RedemptionMode)}
      />
      <Button type="submit" variant="outline" disabled={update.isPending || mode === current}>
        <Save aria-hidden="true" />
        {update.isPending ? tAdmin('form.saving') : tAdmin('form.save')}
      </Button>
    </form>
  );
}
