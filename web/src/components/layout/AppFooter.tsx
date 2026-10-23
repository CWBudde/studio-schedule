import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { useBackendStatus } from '@/api/backendStatus';
import { healthQuery } from '@/api/queries';
import { repositoryUrl } from '@/lib/env';

export function AppFooter() {
  const { t } = useTranslation();
  const backend = useBackendStatus();
  const { data } = useQuery({ ...healthQuery, enabled: backend.state === 'ready' });

  return (
    <footer className="border-t">
      <div className="mx-auto flex max-w-5xl flex-col gap-1 px-4 py-4 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
        <p>
          {t('app.name')} ·{' '}
          {data ? t('footer.version', { version: data.version }) : t('footer.versionUnknown')}
        </p>
        <a
          href={repositoryUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="underline-offset-4 hover:text-foreground hover:underline"
        >
          {t('footer.source')}
        </a>
      </div>
    </footer>
  );
}
