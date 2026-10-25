import { useQuery } from '@tanstack/react-query';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { AuditEventEntry } from '@/api/client';
import { listAuditEventsQuery, meQuery, publicSettingsQuery } from '@/api/queries';
import { formatTimestamp } from '@/lib/schoolTime';
import { DataTable, DataTableLoading } from './crud/DataTable';
import { personName } from '@/lib/personName';
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
  ['mail-requeued', 'audit.actions.mail-requeued'],
  ['person-merged', 'audit.actions.person-merged'],
  ['person-anonymized', 'audit.actions.person-anonymized'],
  ['account-disabled', 'audit.actions.account-disabled'],
  ['account-enabled', 'audit.actions.account-enabled'],
  ['account-role-changed', 'audit.actions.account-role-changed'],
  ['account-deleted', 'audit.actions.account-deleted'],
]);

type KnownAction =
  | 'impersonation-started'
  | 'impersonation-ended'
  | 'mail-requeued'
  | 'person-merged'
  | 'person-anonymized'
  | 'account-disabled'
  | 'account-enabled'
  | 'account-role-changed'
  | 'account-deleted';

type AuditAccount = NonNullable<AuditEventEntry['actor']>;
type AuditPerson = NonNullable<AuditEventEntry['targetPerson']>;

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
            // No actor: the account's holder deleted it themself.
            columns.accessor((e) => (e.actor ? accountText(e.actor) : t('audit.holder')), {
              id: 'actor',
              header: t('audit.actor'),
              cell: (info) => {
                const actor = info.row.original.actor;
                return actor ? (
                  <Account account={actor} />
                ) : (
                  <span className="block font-medium">{t('audit.holder')}</span>
                );
              },
            }),
            columns.accessor(
              (e) => targetText(e, t('audit.mail', { to: e.details?.recipient ?? '' })),
              {
                id: 'target',
                header: t('audit.target'),
                cell: (info) => <Target event={info.row.original} />,
              },
            ),
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

/**
 * What an event concerns, for the search and the sorting: its account, its
 * person, or the mail it names (`mail` is the mail's label); '' for none.
 */
function targetText(e: AuditEventEntry, mail: string) {
  if (e.target) return accountText(e.target);
  if (e.targetPerson) {
    return `${personText(e.targetPerson)} ${e.details?.mergedName ?? ''} ${e.targetPerson.id}`.trimStart();
  }
  if (e.details?.mailId) return `${mail} ${e.details.template ?? ''}`.trimEnd();
  return '';
}

const personText = (p: AuditPerson) =>
  p.givenName === undefined && p.familyName === undefined
    ? ''
    : personName({
        givenName: p.givenName ?? '',
        familyName: p.familyName ?? '',
        anonymized: p.anonymized,
      });

/** The person's name, or '' for an account whose person can no longer be found. */
const accountName = (a: AuditAccount) =>
  a.givenName === undefined && a.familyName === undefined
    ? ''
    : personName({ givenName: a.givenName ?? '', familyName: a.familyName ?? '' });

/** What an event concerns: its account, its person, its mail, or nothing. */
function Target({ event }: { event: AuditEventEntry }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  if (event.target) {
    const { previousRole, role } = event.details ?? {};
    return (
      <>
        <Account account={event.target} />
        {previousRole && role && (
          <span className="block text-xs text-muted-foreground">
            {t('audit.roleChange', {
              from: tAuth(`roles.${previousRole}`),
              to: tAuth(`roles.${role}`),
            })}
          </span>
        )}
      </>
    );
  }
  if (event.targetPerson) {
    const name = personText(event.targetPerson);
    const mergedName = event.details?.mergedName;
    return (
      <>
        {name === '' ? (
          <>
            <span className="block font-medium">{t('audit.unknownPerson')}</span>
            <span className="block text-xs break-all text-muted-foreground">
              {event.targetPerson.id}
            </span>
          </>
        ) : (
          <span className="block font-medium">{name}</span>
        )}
        {mergedName && (
          <span className="block text-xs text-muted-foreground">
            {t('audit.merged', { name: mergedName })}
          </span>
        )}
      </>
    );
  }
  const details = event.details;
  if (details?.mailId) {
    return (
      <>
        <span className="block font-medium break-all">
          {t('audit.mail', { to: details.recipient ?? '' })}
        </span>
        {details.template && (
          <span className="block text-xs text-muted-foreground">{details.template}</span>
        )}
      </>
    );
  }
  return <span aria-label={t('audit.nothing')}>—</span>;
}

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
