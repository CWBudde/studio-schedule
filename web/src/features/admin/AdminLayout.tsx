import { useQuery } from '@tanstack/react-query';
import { Link, Outlet } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { meQuery } from '@/api/queries';
import { Skeleton } from '@/components/ui/skeleton';
import { canAdminister } from './access';
import { LoadError } from './crud/LoadError';
import { NoAccess } from './NoAccess';

const sectionLink =
  'shrink-0 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none data-[status=active]:bg-muted data-[status=active]:text-foreground';

/**
 * The administration (/admin/…): a navigation of the sections the signed-in
 * role may use, and the section. Anyone but the office and admins sees that
 * it is not for them; the API refuses them as well.
 */
export function AdminLayout() {
  const { t } = useTranslation('admin');
  const me = useQuery(meQuery);
  const role = me.data?.role;

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
  if (!canAdminister(role)) return <NoAccess page message={t('noAccess.office')} />;

  return (
    <div className="space-y-6">
      <nav
        aria-label={t('nav.label')}
        className="-mx-1 flex gap-1 overflow-x-auto border-b px-1 pb-2"
      >
        <Link to="/admin/locations" className={sectionLink}>
          {t('nav.locations')}
        </Link>
        <Link to="/admin/course-types" className={sectionLink}>
          {t('nav.courseTypes')}
        </Link>
        <Link to="/admin/staff" className={sectionLink}>
          {t('nav.staff')}
        </Link>
        <Link to="/admin/terms" className={sectionLink}>
          {t('nav.timetable')}
        </Link>
        {role === 'admin' && (
          <Link to="/admin/accounts" className={sectionLink}>
            {t('nav.accounts')}
          </Link>
        )}
      </nav>
      <Outlet />
    </div>
  );
}
