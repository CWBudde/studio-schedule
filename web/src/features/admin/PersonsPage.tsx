import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { Search, UserPlus } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type PersonMatch, personsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { DataTable } from './crud/DataTable';
import { personName } from '@/lib/personName';
import { maxPersonMatches } from './crud/person';
import { columnHelper } from './crud/table';
import { PersonSheet, type PersonTarget } from './PersonSheet';
import { validatePersonsSearch } from './personsSearch';

const columns = columnHelper<PersonMatch>();

/** How long the typing pauses before the search goes into the URL and to the API. */
const searchDelay = 250;

/**
 * The persons of the school (/admin/persons), for the office: a search over
 * names and emails (the API's, at most 50 matches; kept in the URL as `q`,
 * so going back from a person keeps it), each person a link to their page,
 * and a sheet that adds a person.
 */
export function PersonsPage() {
  const { t } = useTranslation('admin');
  const navigate = useNavigate();
  const { q: search = '' } = validatePersonsSearch(useSearch({ strict: false }));
  const [query, setQuery] = useState(search);
  /** The search this page last put into the URL. */
  const [written, setWritten] = useState(search);
  // The URL may change elsewhere (back and forward, the navigation's link):
  // then the box follows. Our own writes land while the typing may go on,
  // so they leave the box alone.
  const [shown, setShown] = useState(search);
  if (search !== shown) {
    setShown(search);
    if (search !== written) {
      setWritten(search);
      setQuery(search);
    }
  }
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const changeQuery = (value: string) => {
    setQuery(value);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      const q = value.trim();
      setWritten(q);
      void navigate({ to: '/admin/persons', search: { q: q || undefined }, replace: true });
    }, searchDelay);
  };
  const persons = useQuery({ ...personsQuery(search), placeholderData: keepPreviousData });
  // The rows of an earlier search are no answer to a failed one.
  const rows = persons.isError ? undefined : persons.data;
  const [sheet, setSheet] = useState<PersonTarget | null>(null);

  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor((p) => personName(p), {
          id: 'name',
          header: t('persons.name'),
          // The API's order (family, then given name) stays.
          enableSorting: false,
          cell: (info) => (
            <Link
              to="/admin/persons/$personId"
              params={{ personId: info.row.original.id }}
              className="font-medium underline-offset-4 hover:underline focus-visible:underline"
            >
              {info.getValue()}
            </Link>
          ),
        }),
        columns.accessor('email', {
          header: t('persons.email'),
          enableSorting: false,
          cell: (info) => (
            <span className="break-all text-muted-foreground">{info.getValue()}</span>
          ),
        }),
        columns.accessor('memberNo', {
          header: t('persons.memberNo'),
          enableSorting: false,
          cell: (info) => <span className="text-muted-foreground">{info.getValue()}</span>,
        }),
      ]),
    [t],
  );

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl font-semibold tracking-tight">
            {t('persons.title')}
          </h1>
          <p className="max-w-prose text-muted-foreground">{t('persons.description')}</p>
        </div>
        <Button size="sm" onClick={() => setSheet({ mode: 'add' })}>
          <UserPlus aria-hidden="true" />
          {t('persons.add')}
        </Button>
      </div>

      <div className="relative max-w-sm">
        <Search
          className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
          aria-hidden="true"
        />
        <Input
          type="search"
          aria-label={t('persons.search')}
          placeholder={t('persons.searchPlaceholder')}
          className="pl-8"
          maxLength={100}
          value={query}
          onChange={(e) => changeQuery(e.target.value)}
        />
      </div>

      <div className="space-y-3">
        <DataTable
          label={t('persons.table')}
          empty={search ? t('persons.noMatches', { query: search }) : t('persons.empty')}
          columns={tableColumns}
          data={rows}
          getRowId={(p) => p.id}
          isError={persons.isError}
          onRetry={() => void persons.refetch()}
        />
        {/* The search stops at the limit, so there may be more matches. */}
        {rows?.length === maxPersonMatches && (
          <p className="text-sm text-muted-foreground">
            {t('persons.more', { count: maxPersonMatches })}
          </p>
        )}
      </div>

      <PersonSheet target={sheet} onClose={() => setSheet(null)} />
    </div>
  );
}
