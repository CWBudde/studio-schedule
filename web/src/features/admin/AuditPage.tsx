import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { AuditEventEntry } from '@/api/client';
import { listAuditEventsQuery, meQuery, publicSettingsQuery } from '@/api/queries';
import { formatTimestamp } from '@/lib/schoolTime';
import { DataTable, DataTableLoading } from './crud/DataTable';
import { personName } from './crud/person';
import { columnHelper } from './crud/table';
import { NoAccess } from './NoAccess';

const columns = columnHelper<AuditEventEntry>();

/** The most events the API lists; mirrors `service.AuditLogLimit`. */
const auditLogLimit = 200;

/**
 * The translation keys of the actions this page knows. Any other action (a
 * later API version may add some) shows as the API names it.
 */
const actionKeys: ReadonlyMap<string, `audit.actions.${KnownAction}`> = new Map([
  ['impersonation-started', 'audit.actions.impersonation-started'],
  ['impersonation-ended', 'audit.actions.impersonation-ended'],
]);

type KnownAction = 'impersonation-started' | 'impersonation-ended';

type AuditAccount = AuditEventEntry['actor'];

/**
 * The audit log (/admin/audit), for admins only: the latest recorded
 * actions, newest first, with a search over them. Everyone else sees that
 * the page is not for them; the API refuses them the events as well.
 */
export function AuditPage() {
  const { t } = useTranslation('admin');
  const me = useQuery(meQuery);
  const isAdmin = me.data?.role === 'admin';

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('audit.title')}</h1>
        {isAdmin && (
          <p className="max-w-prose text-muted-foreground">
            {t('audit.description', { count: auditLogLimit })}
          </p>
        )}
      </div>
      {me.isPending ? (
        <DataTableLoading />
      ) : !isAdmin ? (
        <NoAccess message={t('audit.noAccess')} />
      ) : (
        <AuditTable />
      )}
    </div>
  );
}

function AuditTable() {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const events = useQuery(listAuditEventsQuery);
  const settings = useQuery(publicSettingsQuery);
  const timeZone = settings.data?.timezone;

  const tableColumns = useMemo(
    () =>
      timeZone === undefined
        ? []
        : columns.columns([
            // The formatted time, so that the search finds what the table shows;
            // sorted by the instant.
            columns.accessor((e) => formatTimestamp(e.at, timeZone, lang), {
              id: 'at',
              header: t('audit.at'),
              sortFn: (a, b) => Date.parse(a.original.at) - Date.parse(b.original.at),
              cell: (info) => (
                <time dateTime={info.row.original.at} className="whitespace-nowrap">
                  {info.getValue()}
                </time>
              ),
            }),
            columns.accessor(
              (e) => {
                const key = actionKeys.get(e.action);
                return key === undefined ? e.action : t(key);
              },
              { id: 'action', header: t('audit.action') },
            ),
            columns.accessor((e) => accountText(e.actor), {
              id: 'actor',
              header: t('audit.actor'),
              cell: (info) => <Account account={info.row.original.actor} />,
            }),
            columns.accessor((e) => accountText(e.target), {
              id: 'target',
              header: t('audit.target'),
              cell: (info) => <Account account={info.row.original.target} />,
            }),
          ]),
    [t, lang, timeZone],
  );

  return (
    <DataTable
      label={t('audit.table')}
      searchLabel={t('audit.search')}
      empty={t('audit.empty')}
      columns={tableColumns}
      data={timeZone === undefined ? undefined : events.data}
      getRowId={(e) => e.id}
      isError={events.isError || settings.isError}
      onRetry={() => {
        if (events.isError) void events.refetch();
        if (settings.isError) void settings.refetch();
      }}
      initialSorting={[{ id: 'at', desc: true }]}
    />
  );
}

/**
 * Name and email of an account, for the search and the sorting; the ID
 * as well, which is all an account that can no longer be found still has.
 */
const accountText = (a: AuditAccount) =>
  a.email === undefined ? a.id : `${accountName(a)} ${a.email} ${a.id}`.trimStart();

/** The person's name, or '' for an account whose person can no longer be found. */
const accountName = (a: AuditAccount) =>
  a.givenName === undefined && a.familyName === undefined
    ? ''
    : personName({ givenName: a.givenName ?? '', familyName: a.familyName ?? '' });

function Account({ account }: { account: AuditAccount }) {
  const { t } = useTranslation('admin');
  if (account.email === undefined) {
    return (
      <>
        <span className="block font-medium">{t('audit.unknownAccount')}</span>
        <span className="block text-xs break-all text-muted-foreground">{account.id}</span>
      </>
    );
  }
  const name = accountName(account);
  if (name === '') {
    return <span className="block font-medium break-all">{account.email}</span>;
  }
  return (
    <>
      <span className="block font-medium">{name}</span>
      <span className="block text-xs break-all text-muted-foreground">{account.email}</span>
    </>
  );
}
