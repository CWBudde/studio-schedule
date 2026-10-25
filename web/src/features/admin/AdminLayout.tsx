import { Link, Outlet } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import type { Role } from '@/api/client';
import { canAdminister } from './access';
import { RoleGate } from './RoleGate';

const sectionLink =
  'shrink-0 rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none data-[status=active]:bg-muted data-[status=active]:text-foreground';

/**
 * The administration (/admin/…): a navigation of the sections the signed-in
 * role may use, and the section. Anyone but the office and admins sees that
 * it is not for them; the API refuses them as well.
 */
export function AdminLayout() {
  const { t } = useTranslation('admin');
  return (
    <RoleGate allow={canAdminister} noAccess={t('noAccess.office')}>
      {(me) => <Administration role={me.role} />}
    </RoleGate>
  );
}

function Administration({ role }: { role: Role }) {
  const { t } = useTranslation('admin');
  const { t: tAnnouncements } = useTranslation('announcements');
  const { t: tMailLog } = useTranslation('mailLog');
  const { t: tImports } = useTranslation('imports');
  const { t: tDuplicates } = useTranslation('duplicates');
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
        <Link to="/admin/pass-types" className={sectionLink}>
          {t('nav.passes')}
        </Link>
        <Link to="/admin/staff" className={sectionLink}>
          {t('nav.staff')}
        </Link>
        <Link to="/admin/terms" className={sectionLink}>
          {t('nav.timetable')}
        </Link>
        <Link to="/admin/persons" className={sectionLink}>
          {t('nav.persons')}
        </Link>
        <Link to="/admin/duplicates" className={sectionLink}>
          {tDuplicates('nav')}
        </Link>
        <Link to="/admin/import" className={sectionLink}>
          {tImports('nav')}
        </Link>
        <Link to="/admin/receipts" className={sectionLink}>
          {t('nav.receipts')}
        </Link>
        <Link to="/admin/statistics" className={sectionLink}>
          {t('nav.statistics')}
        </Link>
        <Link to="/admin/announcements" className={sectionLink}>
          {tAnnouncements('nav')}
        </Link>
        {role === 'admin' && (
          <>
            <Link to="/admin/accounts" className={sectionLink}>
              {t('nav.accounts')}
            </Link>
            <Link to="/admin/audit" className={sectionLink}>
              {t('nav.audit')}
            </Link>
            <Link to="/admin/mails" className={sectionLink}>
              {tMailLog('nav')}
            </Link>
          </>
        )}
        <Link to="/admin/settings" className={sectionLink}>
          {t('nav.settings')}
        </Link>
      </nav>
      <Outlet />
    </div>
  );
}
