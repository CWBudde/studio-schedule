import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ImageUp, Save, Trash2 } from 'lucide-react';
import { type ChangeEvent, useEffect, useId, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  deleteReceiptLogoMutation,
  receiptLogoQuery,
  type ReceiptSettings,
  type ReceiptSettingsData,
  receiptSettingsQuery,
  updateReceiptLogoMutation,
  updateReceiptSettingsMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { TextField } from '@/features/auth/TextField';
import { FormError } from '@/features/auth/FormError';
import { SelectField, TextareaField } from './crud/fields';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { LoadError } from './crud/LoadError';
import { Section } from './PersonSection';
import {
  fileBase64,
  fileDataUrl,
  receiptSettingsFields,
  receiptSettingsSchema,
  type ReceiptSettingsValues,
  receiptSettingsValues,
} from './receiptSettingsForms';

const taxModes = ['small_business', 'exempt', 'standard'] as const;

/**
 * "Receipts": who issues them (name, address, tax number, contacts), the
 * tax mode and note, the footer, the prefix of their numbers and the logo,
 * all printed on later receipts. An admin changes them; the office reads
 * them. Until they are complete, no receipt (so no pass sale) can be issued,
 * and the card says what is missing.
 */
export function ReceiptSettingsCard({ admin }: { admin: boolean }) {
  const { t } = useTranslation('receipts');
  const settings = useQuery(receiptSettingsQuery);
  return (
    <Section title={t('settings.title')} description={t('settings.description')}>
      {settings.data ? (
        <>
          {!settings.data.complete && <Incomplete settings={settings.data} />}
          {admin ? (
            <SettingsForm settings={settings.data} />
          ) : (
            <SettingsFacts settings={settings.data} />
          )}
          <Logo settings={settings.data} admin={admin} />
        </>
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

function Incomplete({ settings }: { settings: ReceiptSettings }) {
  const { t } = useTranslation('receipts');
  return (
    <Alert variant="destructive">
      <AlertTitle>{t('incomplete.title')}</AlertTitle>
      {settings.missing && (
        <AlertDescription>
          {t('incomplete.missing', { field: t(`settings.fields.${settings.missing}`) })}
        </AlertDescription>
      )}
    </Alert>
  );
}

function SettingsFacts({ settings }: { settings: ReceiptSettings }) {
  const { t } = useTranslation('receipts');
  const rows: [string, string | undefined][] = [
    [t('settings.fields.issuerName'), settings.issuerName],
    [t('settings.fields.issuerAddress'), settings.issuerAddress],
    [t('settings.fields.taxNumber'), settings.taxNumber],
    [t('settings.fields.email'), settings.email],
    [t('settings.fields.phone'), settings.phone],
    [t('settings.fields.taxMode'), settings.taxMode && t(`settings.taxModes.${settings.taxMode}`)],
    [t('settings.fields.taxNote'), settings.taxNote],
    [t('settings.fields.footer'), settings.footer],
    [t('settings.fields.receiptPrefix'), settings.receiptPrefix],
  ];
  return (
    <div className="space-y-2 text-sm">
      <dl className="grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-[auto_1fr] sm:gap-y-2">
        {rows.map(([term, value]) => (
          <div key={term} className="contents">
            <dt className="text-muted-foreground">{term}</dt>
            <dd className="mb-2 min-w-0 break-words whitespace-pre-line sm:mb-0">
              {value || <span className="text-muted-foreground">—</span>}
            </dd>
          </div>
        ))}
      </dl>
      <p className="text-muted-foreground">{t('settings.adminOnly')}</p>
    </div>
  );
}

function SettingsForm({ settings }: { settings: ReceiptSettings }) {
  const { t } = useTranslation('receipts');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const update = useMutation(updateReceiptSettingsMutation(queryClient));
  const form = useForm<ReceiptSettingsValues, unknown, ReceiptSettingsData>({
    resolver: zodResolver(receiptSettingsSchema),
    defaultValues: receiptSettingsValues(settings),
  });
  const { errors } = form.formState;
  const optional = (label: string) => tAdmin('form.optional', { label });

  const onSubmit = (body: ReceiptSettingsData) =>
    update.mutate(body, {
      onSuccess: (saved) => {
        form.reset(receiptSettingsValues(saved));
        toast.success(saved.complete ? t('settings.saved') : t('settings.savedIncomplete'));
      },
      onError: (error) => showServerErrors(error, form, receiptSettingsFields),
    });

  return (
    <form
      noValidate
      aria-label={t('settings.title')}
      className="space-y-4"
      onSubmit={form.handleSubmit(onSubmit)}
    >
      <ProblemAlert error={update.error} messages={{ 422: tAuth('form.checkEntries') }} />
      <TextField
        label={t('settings.fields.issuerName')}
        autoComplete="off"
        error={errorText(errors.issuerName)}
        {...form.register('issuerName')}
      />
      <TextareaField
        label={t('settings.fields.issuerAddress')}
        rows={3}
        error={errorText(errors.issuerAddress)}
        {...form.register('issuerAddress')}
      />
      <TextField
        label={t('settings.fields.taxNumber')}
        autoComplete="off"
        error={errorText(errors.taxNumber)}
        {...form.register('taxNumber')}
      />
      <Controller
        control={form.control}
        name="taxMode"
        render={({ field }) => (
          <SelectField
            label={t('settings.fields.taxMode')}
            placeholder={t('settings.chooseTaxMode')}
            hint={field.value ? t(`settings.taxModeHints.${field.value}`) : undefined}
            options={taxModes.map((m) => ({ value: m, label: t(`settings.taxModes.${m}`) }))}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.taxMode)}
          />
        )}
      />
      <TextareaField
        label={optional(t('settings.fields.taxNote'))}
        rows={2}
        hint={t('settings.taxNoteHint')}
        error={errorText(errors.taxNote)}
        {...form.register('taxNote')}
      />
      <TextField
        label={optional(t('settings.fields.email'))}
        type="email"
        autoComplete="off"
        error={errorText(errors.email)}
        {...form.register('email')}
      />
      <TextField
        label={optional(t('settings.fields.phone'))}
        autoComplete="off"
        error={errorText(errors.phone)}
        {...form.register('phone')}
      />
      <TextareaField
        label={optional(t('settings.fields.footer'))}
        rows={2}
        hint={t('settings.footerHint')}
        error={errorText(errors.footer)}
        {...form.register('footer')}
      />
      <TextField
        label={t('settings.fields.receiptPrefix')}
        autoComplete="off"
        maxLength={16}
        hint={t('settings.prefixHint')}
        error={errorText(errors.receiptPrefix)}
        {...form.register('receiptPrefix')}
      />
      <p className="text-sm text-muted-foreground">{t('settings.notCashRegister')}</p>
      <Button type="submit" variant="outline" disabled={update.isPending}>
        <Save aria-hidden="true" />
        {update.isPending ? tAdmin('form.saving') : tAdmin('form.save')}
      </Button>
    </form>
  );
}

/** The logo of later receipts: shown, and for an admin replaced or removed. */
function Logo({ settings, admin }: { settings: ReceiptSettings; admin: boolean }) {
  const { t } = useTranslation('receipts');
  const queryClient = useQueryClient();
  const inputId = useId();
  const logo = useQuery({ ...receiptLogoQuery, enabled: settings.logoType !== undefined });
  const upload = useMutation(updateReceiptLogoMutation(queryClient));
  const remove = useMutation(deleteReceiptLogoMutation(queryClient));
  /** Why the chosen file was not sent: not a PNG or JPEG or too large, or unreadable. */
  const [refused, setRefused] = useState<'invalid' | 'unreadable'>();
  const url = useDataUrl(settings.logoType ? logo.data : null);
  const pending = upload.isPending || remove.isPending;
  const kilobytes = Math.floor(settings.logoMaxBytes / 1024);

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file) return;
    // Only the answer of this attempt is shown.
    upload.reset();
    remove.reset();
    const type = file.type === 'image/png' || file.type === 'image/jpeg' ? file.type : undefined;
    if (file.size > settings.logoMaxBytes || !type) {
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
    upload.mutate({ contentType: type, data }, { onSuccess: () => toast.success(t('logo.saved')) });
  };

  const onRemove = () => {
    upload.reset();
    setRefused(undefined);
    remove.mutate(undefined, { onSuccess: () => toast.success(t('logo.removed')) });
  };

  return (
    <div className="space-y-3 border-t pt-4">
      <h3 className="text-sm font-medium">{t('logo.title')}</h3>
      {url ? (
        <img
          src={url}
          alt={t('logo.alt')}
          className="max-h-24 max-w-full rounded border bg-white object-contain p-2"
        />
      ) : (
        <p className="text-sm text-muted-foreground">
          {settings.logoType ? t('logo.loading') : t('logo.none')}
        </p>
      )}
      {admin && (
        <>
          {refused && (
            <p role="alert" className="text-sm text-destructive">
              {t(`logo.${refused}`, { size: kilobytes })}
            </p>
          )}
          <FormError error={upload.error ?? remove.error} />
          <div className="flex flex-wrap items-center gap-2">
            <input
              id={inputId}
              type="file"
              accept="image/png,image/jpeg"
              className="peer sr-only"
              disabled={pending}
              onChange={(e) => void onFile(e)}
            />
            <Button asChild variant="outline" size="sm" disabled={pending}>
              <label
                htmlFor={inputId}
                className="cursor-pointer peer-focus-visible:ring-[3px] peer-focus-visible:ring-ring/50"
              >
                <ImageUp aria-hidden="true" />
                {upload.isPending
                  ? t('logo.uploading')
                  : settings.logoType
                    ? t('logo.replace')
                    : t('logo.upload')}
              </label>
            </Button>
            {settings.logoType && (
              <Button variant="outline" size="sm" disabled={pending} onClick={onRemove}>
                <Trash2 aria-hidden="true" />
                {t('logo.remove')}
              </Button>
            )}
          </div>
          <p className="text-xs text-muted-foreground">{t('logo.hint', { size: kilobytes })}</p>
        </>
      )}
    </div>
  );
}

/**
 * `blob` as a data URL, once it is read; nothing to revoke, so it is safe
 * however often effects run.
 */
function useDataUrl(blob: Blob | null | undefined): string | undefined {
  const [read, setRead] = useState<{ blob: Blob; url: string }>();
  useEffect(() => {
    if (!blob) return;
    let current = true;
    fileDataUrl(blob).then(
      (url) => current && setRead({ blob, url }),
      () => undefined,
    );
    return () => {
      current = false;
    };
  }, [blob]);
  return blob && read?.blob === blob ? read.url : undefined;
}
