import { Link } from '@tanstack/react-router';
import { CircleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { ReceiptSettings } from '@/api/queries';
import { useMe } from '@/api/useMe';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';

/**
 * Receipts cannot be issued (so no pass can be sold, and no receipt
 * canceled) until the receipt settings are complete: what is `missing`, and
 * a link to the settings for an admin, or to ask one for the office.
 */
export function ReceiptSettingsNotice({ missing }: { missing?: ReceiptSettings['missing'] }) {
  const { t } = useTranslation('receipts');
  const me = useMe();
  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{t('incomplete.title')}</AlertTitle>
      <AlertDescription>
        {missing && <p>{t('incomplete.missing', { field: t(`settings.fields.${missing}`) })}</p>}
        {me.data?.role === 'admin' ? (
          <Link
            to="/admin/settings"
            className="font-medium text-foreground underline underline-offset-4"
          >
            {t('incomplete.open')}
          </Link>
        ) : (
          <p>{t('incomplete.askAdmin')}</p>
        )}
      </AlertDescription>
    </Alert>
  );
}
