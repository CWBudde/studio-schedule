import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Save } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type SchoolSettings,
  schoolSettingsQuery,
  updateSchoolSettingsMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { TextField } from '@/features/auth/TextField';
import { TextareaField } from './crud/fields';
import { showServerErrors } from './crud/form';
import { Section } from './PersonSection';
import { schoolFields, schoolSchema, type SchoolValues, schoolValues } from './settingsForms';
import { useSettingsErrorText } from './settingsErrors';
import { SettingsFacts, SettingsLoaded } from './settingsShared';

/**
 * "School": the name (header, browser tab, mails) and the legal notice
 * (footer). An admin changes them; the office reads them.
 */
export function SchoolSettingsCard({ admin }: { admin: boolean }) {
  const { t } = useTranslation('settings');
  const query = useQuery(schoolSettingsQuery);
  return (
    <Section title={t('school.title')} description={t('school.description')}>
      <SettingsLoaded query={query}>
        {(settings) =>
          admin ? (
            <SchoolForm settings={settings} />
          ) : (
            <SettingsFacts
              rows={[
                [t('school.name'), settings.schoolName],
                [t('school.legalNotice'), settings.legalNotice],
              ]}
            />
          )
        }
      </SettingsLoaded>
    </Section>
  );
}

function SchoolForm({ settings }: { settings: SchoolSettings }) {
  const { t } = useTranslation('settings');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useSettingsErrorText();
  const queryClient = useQueryClient();
  const update = useMutation(updateSchoolSettingsMutation(queryClient));
  const form = useForm<SchoolValues, unknown, SchoolSettings>({
    resolver: zodResolver(schoolSchema),
    defaultValues: schoolValues(settings),
  });
  const { errors } = form.formState;

  const onSubmit = (body: SchoolSettings) =>
    update.mutate(body, {
      onSuccess: (saved) => {
        form.reset(schoolValues(saved));
        toast.success(t('saved'));
      },
      onError: (error) => showServerErrors(error, form, schoolFields),
    });

  return (
    <form
      noValidate
      aria-label={t('school.title')}
      className="space-y-4"
      onSubmit={form.handleSubmit(onSubmit)}
    >
      <ProblemAlert error={update.error} messages={{ 422: tAuth('form.checkEntries') }} />
      <TextField
        label={t('school.name')}
        autoComplete="organization"
        maxLength={200}
        error={errorText(errors.schoolName)}
        {...form.register('schoolName')}
      />
      <TextareaField
        label={tAdmin('form.optional', { label: t('school.legalNotice') })}
        rows={4}
        maxLength={2000}
        hint={t('school.legalNoticeHint')}
        error={errorText(errors.legalNotice)}
        {...form.register('legalNotice')}
      />
      <Button type="submit" variant="outline" disabled={update.isPending}>
        <Save aria-hidden="true" />
        {update.isPending ? tAdmin('form.saving') : tAdmin('form.save')}
      </Button>
    </form>
  );
}
