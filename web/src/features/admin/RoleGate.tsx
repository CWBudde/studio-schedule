import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { Me, Role } from '@/api/client';
import { useMe } from '@/api/useMe';
import { Skeleton } from '@/components/ui/skeleton';
import { LoadError } from './crud/LoadError';
import { NoAccess } from './NoAccess';

/**
 * Shows a page only to the roles `allow` accepts: while the signed-in
 * account is unknown a placeholder, if asking failed an error to retry, and
 * anyone else (a visitor too) `noAccess` as the page's title text. The API
 * refuses them as well; this only spares them a page of errors.
 */
export function RoleGate({
  allow,
  noAccess,
  children,
}: {
  allow: (role: Role | undefined) => boolean;
  /** Who may see the page, e.g. "Only the office and administrators can see this page." */
  noAccess: string;
  children: (me: Me) => ReactNode;
}) {
  const { t } = useTranslation('admin');
  const me = useMe();

  if (me.isPending) {
    return (
      <div role="status" className="space-y-3">
        <span className="sr-only">{t('loading')}</span>
        <Skeleton className="h-9 w-48" aria-hidden="true" />
        <Skeleton className="h-40 w-full" aria-hidden="true" />
      </div>
    );
  }
  // A failed check (network, 5xx) is no answer about the role; signed out is null, not an error.
  if (me.data === undefined && me.isError) {
    return <LoadError message={t('access.loadError')} onRetry={() => void me.refetch()} />;
  }
  if (!me.data || !allow(me.data.role)) return <NoAccess page message={noAccess} />;
  return children(me.data);
}
