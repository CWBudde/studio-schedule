import type { UseQueryResult } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { LoadError } from './crud/LoadError';

/** A card's content once its settings are loaded, else the loading state or the error. */
export function SettingsLoaded<T>({
  query,
  children,
}: {
  query: UseQueryResult<T>;
  children: (data: T) => ReactNode;
}) {
  const { t } = useTranslation('settings');
  if (query.data !== undefined) return children(query.data);
  if (query.isError) {
    return <LoadError message={t('loadError')} onRetry={() => void query.refetch()} />;
  }
  return (
    <p role="status" className="text-sm text-muted-foreground">
      {t('loading')}
    </p>
  );
}

/** The office's read-only view of a card: its settings as a list, and who may change them. */
export function SettingsFacts({
  rows,
  children,
}: {
  rows: [string, ReactNode][];
  children?: ReactNode;
}) {
  const { t } = useTranslation('settings');
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
      {children}
      <p className="text-muted-foreground">{t('adminOnly')}</p>
    </div>
  );
}
