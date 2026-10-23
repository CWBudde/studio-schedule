import { useQuery } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type CourseType, courseTypesQuery, deleteCourseTypeMutation } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { CourseTypeSheet, type CourseTypeTarget } from './CourseTypeSheet';
import { DeleteDialog } from './crud/DeleteDialog';
import { DataTable } from './crud/DataTable';
import { RowActions } from './crud/RowActions';
import { columnHelper } from './crud/table';

const columns = columnHelper<CourseType>();

/**
 * The course types of the school (/admin/course-types), for the office: add,
 * rename, describe and delete them. The API refuses to delete one that
 * classes or lessons use, and the page says why.
 */
export function CourseTypesPage() {
  const { t } = useTranslation('admin');
  const courseTypes = useQuery(courseTypesQuery);
  const [sheet, setSheet] = useState<CourseTypeTarget | null>(null);
  const [deleting, setDeleting] = useState<CourseType | null>(null);

  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor('name', {
          header: t('courseTypes.name'),
          cell: (info) => <span className="font-medium">{info.getValue()}</span>,
        }),
        columns.accessor('description', {
          header: t('courseTypes.details'),
          sortUndefined: 'last',
          cell: (info) => (
            <span className="line-clamp-3 whitespace-pre-line text-muted-foreground">
              {info.getValue()}
            </span>
          ),
        }),
      ]),
    [t],
  );

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl font-semibold tracking-tight">
            {t('courseTypes.title')}
          </h1>
          <p className="max-w-prose text-muted-foreground">{t('courseTypes.description')}</p>
        </div>
        <Button size="sm" onClick={() => setSheet({ mode: 'add' })}>
          <Plus aria-hidden="true" />
          {t('courseTypes.add')}
        </Button>
      </div>

      <DataTable
        label={t('courseTypes.table')}
        searchLabel={t('courseTypes.search')}
        empty={t('courseTypes.empty')}
        columns={tableColumns}
        data={courseTypes.data}
        getRowId={(c) => c.id}
        isError={courseTypes.isError}
        onRetry={() => void courseTypes.refetch()}
        initialSorting={[{ id: 'name', desc: false }]}
        actions={(courseType) => (
          <RowActions
            editLabel={t('courseTypes.editLabel', { name: courseType.name })}
            deleteLabel={t('courseTypes.deleteLabel', { name: courseType.name })}
            onEdit={() => setSheet({ mode: 'edit', courseType })}
            onDelete={() => setDeleting(courseType)}
          />
        )}
      />

      <CourseTypeSheet target={sheet} onClose={() => setSheet(null)} />
      <DeleteDialog
        target={deleting}
        id={(c) => c.id}
        mutation={deleteCourseTypeMutation}
        texts={({ name }) => ({
          title: t('courseTypes.deleteTitle', { name }),
          description: t('courseTypes.deleteDescription'),
          inUse: t('courseTypes.inUse', { name }),
          notFound: t('courseTypes.notFound'),
          deleted: t('courseTypes.deleted', { name }),
        })}
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}
