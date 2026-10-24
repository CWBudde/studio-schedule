import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarDays, CalendarPlus } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { deleteTermMutation, termsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { type Term, termRange } from '@/features/timetable/timetable';
import { DataTable } from './crud/DataTable';
import { DeleteDialog } from './crud/DeleteDialog';
import { RowActions } from './crud/RowActions';
import { columnHelper } from './crud/table';
import { TermSheet, type TermTarget } from './TermSheet';

const columns = columnHelper<Term>();

/**
 * The terms of the timetable (/admin/terms), for the office: add, rename
 * and move them, delete those without classes, and open their weekly
 * classes. The API refuses dates that would leave out a lesson that has to
 * stay, and a delete while the term has classes; the page says why.
 */
export function TermsPage() {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const terms = useQuery(termsQuery);
  const [sheet, setSheet] = useState<TermTarget | null>(null);
  const [deleting, setDeleting] = useState<Term | null>(null);

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
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('terms.title')}</h1>
          <p className="max-w-prose text-muted-foreground">{t('terms.description')}</p>
        </div>
        <Button size="sm" onClick={() => setSheet({ mode: 'add' })}>
          <CalendarPlus aria-hidden="true" />
          {t('terms.add')}
        </Button>
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
          <>
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
            <RowActions
              editLabel={t('terms.editLabel', { name: term.name })}
              deleteLabel={t('terms.deleteLabel', { name: term.name })}
              onEdit={() => setSheet({ mode: 'edit', term })}
              onDelete={() => setDeleting(term)}
            />
          </>
        )}
      />

      <TermSheet target={sheet} onClose={() => setSheet(null)} />
      <DeleteDialog
        target={deleting}
        id={(term) => term.id}
        mutation={deleteTermMutation}
        texts={({ name }) => ({
          title: t('terms.deleteTitle', { name }),
          description: t('terms.deleteDescription'),
          inUse: t('terms.inUse', { name }),
          notFound: t('terms.notFound'),
          deleted: t('terms.deleted', { name }),
        })}
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}
