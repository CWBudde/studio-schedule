import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { ArrowLeft, KeyRound, Merge, Pencil, Trash2 } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, deletePersonMutation, type PersonDetail, personQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { TimetableEmpty } from '@/features/timetable/TimetableStates';
import { weekdayName } from '@/features/timetable/timetable';
import { formatDate } from '@/lib/schoolTime';
import { DeleteDialog } from './crud/DeleteDialog';
import { LoadError } from './crud/LoadError';
import { personName } from './crud/person';
import { PersonAccountSheet } from './PersonAccountSheet';
import { PersonGuardians } from './PersonGuardians';
import { PersonPasses } from './PersonPasses';
import { Section } from './PersonSection';
import { PersonSheet, type PersonTarget } from './PersonSheet';

/** The page of the route: the person in the URL, afresh for every person. */
export function PersonRoute() {
  const { personId } = useParams({ from: '/admin/persons/$personId' });
  return <PersonPage key={personId} personId={personId} />;
}

const linkClass =
  'font-medium underline-offset-4 hover:underline focus-visible:underline break-words';

/**
 * One person for the office (/admin/persons/$personId): their data (edited
 * in a sheet), guardians (linked and unlinked here), children, account
 * (created here with an invitation), passes (sold, given and voided here),
 * staff links and class memberships. A person nothing refers to any more
 * can be deleted; the list follows.
 */
export function PersonPage({ personId }: { personId: string }) {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const person = useQuery(personQuery(personId));
  const [editing, setEditing] = useState<PersonTarget | null>(null);
  const [creatingAccount, setCreatingAccount] = useState<PersonDetail | null>(null);
  const [deleting, setDeleting] = useState<PersonDetail | null>(null);

  const back = (
    <Link
      to="/admin/persons"
      className="inline-flex items-center gap-1 text-sm font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
    >
      <ArrowLeft className="size-4" aria-hidden="true" />
      {t('persons.detail.all')}
    </Link>
  );

  if (person.data === undefined) {
    return (
      <div className="space-y-4">
        {back}
        {person.error instanceof ApiError && person.error.status === 404 ? (
          <TimetableEmpty
            title={t('persons.detail.noPerson')}
            description={t('persons.detail.noPersonDescription')}
          />
        ) : person.isError ? (
          <LoadError
            message={t('persons.detail.loadError')}
            onRetry={() => void person.refetch()}
          />
        ) : (
          <div role="status" className="space-y-3">
            <span className="sr-only">{t('loading')}</span>
            <Skeleton className="h-9 w-56" aria-hidden="true" />
            <Skeleton className="h-48 w-full" aria-hidden="true" />
          </div>
        )}
      </div>
    );
  }

  const p = person.data;
  const name = personName(p);
  if (p.mergedInto) {
    // A merged person is a record kept for its receipts (ADR 0015).
    return (
      <div className="space-y-6">
        <div className="space-y-3">
          {back}
          <h1 className="min-w-0 font-heading text-3xl font-semibold tracking-tight break-words">
            {name}
          </h1>
        </div>
        <Alert>
          <Merge aria-hidden="true" />
          <AlertTitle>{t('persons.mergedInto.title')}</AlertTitle>
          <AlertDescription>
            <p>
              {t('persons.mergedInto.description')}{' '}
              <Link
                to="/admin/persons/$personId"
                params={{ personId: p.mergedInto.id }}
                className={linkClass}
              >
                {t('persons.mergedInto.open', { name: personName(p.mergedInto) })}
              </Link>
            </p>
          </AlertDescription>
        </Alert>
        <div className="grid items-start gap-6 lg:grid-cols-2">
          <Details person={p} />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        {back}
        <div className="flex flex-wrap items-end justify-between gap-4">
          <h1 className="min-w-0 font-heading text-3xl font-semibold tracking-tight break-words">
            {name}
          </h1>
          <Button
            variant="outline"
            size="sm"
            aria-label={t('persons.delete.label', { name })}
            onClick={() => setDeleting(p)}
          >
            <Trash2 aria-hidden="true" />
            {t('persons.delete.button')}
          </Button>
        </div>
      </div>
      <div className="grid items-start gap-6 lg:grid-cols-2">
        <Details person={p} onEdit={() => setEditing({ mode: 'edit', person: p })} />
        <AccountSection person={p} onCreate={() => setCreatingAccount(p)} />
        <PersonGuardians person={p} />
        <Dependents person={p} />
        <PersonPasses person={p} />
        <ClassesSection person={p} />
        <StaffSection person={p} />
      </div>
      <PersonSheet target={editing} onClose={() => setEditing(null)} />
      <PersonAccountSheet target={creatingAccount} onClose={() => setCreatingAccount(null)} />
      <DeleteDialog
        target={deleting}
        id={(d) => d.id}
        mutation={deletePersonMutation}
        texts={(d) => {
          const deletedName = personName(d);
          return {
            title: t('persons.delete.title', { name: deletedName }),
            description: t('persons.delete.description'),
            inUse: t('persons.delete.inUse', { name: deletedName }),
            notFound: t('persons.notFound'),
            deleted: t('persons.delete.deleted', { name: deletedName }),
          };
        }}
        onDeleted={(d) =>
          void navigate({ to: '/admin/persons' }).then(() =>
            queryClient.removeQueries({ queryKey: personQuery(d.id).queryKey }),
          )
        }
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}

/** Lines of terms and their values; a missing value shows as a dash. */
function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-1 gap-x-6 gap-y-1 text-sm sm:grid-cols-[auto_1fr] sm:gap-y-2">
      {rows.map(([term, value]) => (
        <div key={term} className="contents">
          <dt className="text-muted-foreground">{term}</dt>
          <dd className="mb-2 min-w-0 break-words whitespace-pre-line sm:mb-0">
            {value || <span className="text-muted-foreground">—</span>}
          </dd>
        </div>
      ))}
    </dl>
  );
}

/** The person's fields; editable with `onEdit` (a merged person is not). */
function Details({ person, onEdit }: { person: PersonDetail; onEdit?: () => void }) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  return (
    <Section
      title={t('persons.detail.details')}
      action={
        onEdit && (
          <Button
            variant="outline"
            size="sm"
            aria-label={t('persons.detail.editLabel', { name: personName(person) })}
            onClick={onEdit}
          >
            <Pencil aria-hidden="true" />
            {t('persons.detail.edit')}
          </Button>
        )
      }
    >
      <Facts
        rows={[
          [t('persons.fields.givenName'), person.givenName],
          [t('persons.fields.familyName'), person.familyName],
          [t('persons.fields.email'), person.email],
          [t('persons.fields.phone'), person.phone],
          [
            t('persons.fields.birthDate'),
            person.birthDate &&
              formatDate(person.birthDate, lang, {
                day: 'numeric',
                month: 'long',
                year: 'numeric',
              }),
          ],
          [t('persons.fields.memberNo'), person.memberNo],
          [t('persons.fields.notes'), person.notes],
          ...(person.merged.length > 0
            ? [
                [
                  t('persons.detail.merged'),
                  person.merged.map((m) => (
                    <Link
                      key={m.id}
                      to="/admin/persons/$personId"
                      params={{ personId: m.id }}
                      className={`${linkClass} block`}
                    >
                      {personName(m)}
                    </Link>
                  )),
                ] satisfies [string, ReactNode],
              ]
            : []),
        ]}
      />
    </Section>
  );
}

function AccountSection({ person, onCreate }: { person: PersonDetail; onCreate: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const name = personName(person);
  const account = person.account;
  return (
    <Section
      title={t('persons.account.title')}
      description={t('persons.account.description', { name })}
      action={
        !account && (
          <Button variant="outline" size="sm" onClick={onCreate}>
            <KeyRound aria-hidden="true" />
            {t('persons.account.create')}
          </Button>
        )
      }
    >
      {account ? (
        <>
          <Facts
            rows={[
              [t('persons.account.email'), account.email],
              [t('persons.account.role'), tAuth(`roles.${account.role}`)],
              [
                t('persons.account.status'),
                <Badge
                  key="status"
                  variant={
                    account.status === 'active'
                      ? 'secondary'
                      : account.status === 'disabled'
                        ? 'destructive'
                        : 'outline'
                  }
                >
                  {t(`persons.account.statuses.${account.status}`)}
                </Badge>,
              ],
            ]}
          />
          {account.status === 'pending' && (
            <p className="text-sm text-muted-foreground">
              {t('persons.account.pendingHint', { givenName: person.givenName })}
            </p>
          )}
        </>
      ) : (
        <p className="text-sm text-muted-foreground">{t('persons.account.none', { name })}</p>
      )}
    </Section>
  );
}

function Dependents({ person }: { person: PersonDetail }) {
  const { t } = useTranslation('admin');
  const name = personName(person);
  return (
    <Section
      title={t('persons.dependents.title')}
      description={t('persons.dependents.description', { name })}
    >
      {person.dependents.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('persons.dependents.empty', { name })}</p>
      ) : (
        <ul aria-label={t('persons.dependents.list')} className="divide-y rounded-lg border">
          {person.dependents.map((d) => (
            <li key={d.id} className="px-3 py-2.5">
              <Link to="/admin/persons/$personId" params={{ personId: d.id }} className={linkClass}>
                {personName(d)}
              </Link>
              {d.email && <p className="truncate text-xs text-muted-foreground">{d.email}</p>}
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}

function ClassesSection({ person }: { person: PersonDetail }) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const name = personName(person);
  return (
    <Section
      title={t('persons.classes.title')}
      description={t('persons.classes.description', { name })}
    >
      {person.classes.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('persons.classes.empty', { name })}</p>
      ) : (
        <ul className="divide-y rounded-lg border">
          {person.classes.map((c) => {
            const label = t('persons.classes.label', {
              courseType: c.courseTypeName,
              day: weekdayName(c.weekday, lang),
              time: c.startTime,
            });
            return (
              <li key={c.classId} className="space-y-0.5 px-3 py-2.5">
                <div className="flex flex-wrap items-center gap-2">
                  <Link
                    to="/admin/terms/$termId/classes"
                    params={{ termId: c.termId }}
                    aria-label={t('persons.classes.openLabel', { label, term: c.termName })}
                    className={linkClass}
                  >
                    {label}
                  </Link>
                  {c.retired && <Badge variant="outline">{t('persons.classes.retired')}</Badge>}
                </div>
                <p className="text-xs text-muted-foreground">{c.termName}</p>
                {c.since && (
                  <p className="text-xs text-muted-foreground tabular-nums">
                    {t('persons.classes.since', {
                      date: formatDate(c.since, lang, {
                        day: 'numeric',
                        month: 'short',
                        year: 'numeric',
                      }),
                    })}
                  </p>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Section>
  );
}

function StaffSection({ person }: { person: PersonDetail }) {
  const { t } = useTranslation('admin');
  const name = personName(person);
  return (
    <Section
      title={t('persons.staff.title')}
      description={t('persons.staff.description', { name })}
      action={
        person.staff.length > 0 && (
          <Link to="/admin/staff" className={`${linkClass} text-sm`}>
            {t('persons.staff.open')}
          </Link>
        )
      }
    >
      {person.staff.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('persons.staff.empty', { name })}</p>
      ) : (
        <ul className="divide-y rounded-lg border">
          {person.staff.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2.5">
              <span className="font-medium break-words">{s.displayName}</span>
              <span className="text-sm text-muted-foreground">{t(`staff.kinds.${s.kind}`)}</span>
              {!s.active && <Badge variant="outline">{t('persons.staff.inactive')}</Badge>}
            </li>
          ))}
        </ul>
      )}
    </Section>
  );
}
