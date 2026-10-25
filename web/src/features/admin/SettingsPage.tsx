import { useTranslation } from 'react-i18next';
import { useMe } from '@/api/useMe';
import { PassSettingsCard } from './PassSettingsCard';
import { ReceiptSettingsCard } from './ReceiptSettingsCard';

/**
 * The school's settings (/admin/settings), a card each: when bookings use a
 * pass, and what receipts print. The office reads them; an admin changes them.
 */
export function SettingsPage() {
  const { t } = useTranslation('passes');
  const me = useMe();
  const admin = me.data?.role === 'admin';
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">
          {t('settings.pageTitle')}
        </h1>
        <p className="max-w-prose text-muted-foreground">
          {admin ? t('settings.pageDescription') : t('settings.pageDescriptionOffice')}
        </p>
      </div>
      <div className="grid items-start gap-6 lg:grid-cols-2">
        <PassSettingsCard admin={admin} />
        <ReceiptSettingsCard admin={admin} />
      </div>
    </div>
  );
}
