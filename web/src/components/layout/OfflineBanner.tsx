import { WifiOff } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useOnline } from '@/lib/online';

/**
 * Says that the app is offline (ADR 0017): it shows what it saved last,
 * and takes no changes until the connection is back.
 */
export function OfflineBanner() {
  const { t } = useTranslation();
  const online = useOnline();
  if (online) return null;

  return (
    <div
      role="status"
      aria-live="polite"
      className="border-b border-border bg-muted text-foreground"
    >
      <div className="mx-auto flex w-full max-w-5xl items-start gap-3 px-4 py-2.5 text-sm leading-snug">
        <WifiOff className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        <p>
          <span className="font-medium">{t('offline.title')}.</span> {t('offline.description')}
        </p>
      </div>
    </div>
  );
}
