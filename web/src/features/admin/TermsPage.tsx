import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarDays } from 'lucide-react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { termsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { type Term, termRange } from '@/features/timetable/timetable';
import { DataTable } from './crud/DataTable';
import { columnHelper } from './crud/table';

const columns = columnHelper<Term>();

/**
 * The terms of the timetable (/admin/terms), for the office: each links to
 * its weekly classes.
 */
export function TermsPage() {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const terms = useQuery(termsQuery);

  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor('name', {
          header: t('terms.name'),
          cell: (info) => <span className="font-medium">{info.getValue()}</span>,
        }),
        columns.accessor('startsOn', {
          header: t('terms.period'),
          cell: (info) => (
            <span className="tabular-nums">
              {t('terms.range', termRange(info.row.original, lang))}
            </span>
          ),
        }),
      ]),
    [t, lang],
  );

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('terms.title')}</h1>
        <p className="max-w-prose text-muted-foreground">{t('terms.description')}</p>
      </div>

      <DataTable
        label={t('terms.table')}
        searchLabel={t('terms.search')}
        empty={t('terms.empty')}
        columns={tableColumns}
        data={terms.data}
        getRowId={(term) => term.id}
        isError={terms.isError}
        onRetry={() => void terms.refetch()}
        actions={(term) => (
          <Button variant="outline" size="sm" asChild>
            <Link
              to="/admin/terms/$termId/classes"
              params={{ termId: term.id }}
              aria-label={t('terms.classesLabel', { name: term.name })}
            >
              <CalendarDays aria-hidden="true" />
              {t('terms.classes')}
            </Link>
          </Button>
        )}
      />
    </div>
  );
}
