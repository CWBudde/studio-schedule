import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  ApiError,
  deleteLevelMutation,
  type Level,
  levelsQuery,
  setLevelOrderMutation,
} from '@/api/queries';
import { Button } from '@/components/ui/button';
import { moveItem } from '@/lib/reorder';
import { DeleteDialog } from './crud/DeleteDialog';
import { DataTable } from './crud/DataTable';
import { RowActions } from './crud/RowActions';
import { columnHelper } from './crud/table';
import { LevelSheet, type LevelTarget } from './LevelSheet';
import { MoveButtons } from './MoveButtons';

/** A level with its place in the API's order, `index` of `count`. */
type LevelRow = Level & { index: number; count: number };

const columns = columnHelper<LevelRow>();

/**
 * The school's level scale (/admin/levels), for the office: add, rename,
 * order and delete the levels. The table keeps the API's order, least to
 * most advanced, so it has no sorting of its own; a move swaps a level with
 * its neighbour and sends the whole new order. The API refuses to delete a
 * level that classes use, and the page says why.
 */
export function LevelsPage() {
  const { t } = useTranslation('admin');
  const levels = useQuery(levelsQuery);
  const [sheet, setSheet] = useState<LevelTarget | null>(null);
  const [deleting, setDeleting] = useState<Level | null>(null);

  const rows = useMemo<LevelRow[] | undefined>(
    () => levels.data?.map((level, index, all) => ({ ...level, index, count: all.length })),
    [levels.data],
  );

  const queryClient = useQueryClient();
  const { mutate: saveOrder, isPending: moving } = useMutation(setLevelOrderMutation(queryClient));
  /** Sends the whole new order, with `level` moved by `delta`. */
  const move = useCallback(
    (level: LevelRow, delta: -1 | 1) => {
      saveOrder(
        moveItem(levels.data ?? [], level.index, delta).map((l) => l.id),
        {
          // The mutation asks for the levels again either way.
          onError: (error) =>
            toast.error(
              error instanceof ApiError && error.status === 422
                ? t('levels.orderStale')
                : t('levels.orderFailed'),
            ),
        },
      );
    },
    [levels.data, saveOrder, t],
  );

  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor('name', {
          header: t('levels.name'),
          enableSorting: false,
          cell: (info) => <span className="font-medium">{info.getValue()}</span>,
        }),
        columns.accessor('code', {
          header: t('levels.code'),
          enableSorting: false,
          cell: (info) => <span className="text-muted-foreground">{info.getValue()}</span>,
        }),
        columns.accessor((level) => level.index, {
          id: 'order',
          header: t('levels.order'),
          enableSorting: false,
          enableGlobalFilter: false,
          cell: (info) => {
            const level = info.row.original;
            return (
              <div className="flex items-center gap-2">
                <span className="w-5 text-right tabular-nums">{level.index + 1}</span>
                <MoveButtons
                  name={level.name}
                  index={level.index}
                  count={level.count}
                  disabled={moving}
                  onMove={(delta) => move(level, delta)}
                />
              </div>
            );
          },
        }),
      ]),
    [t, moving, move],
  );

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl font-semibold tracking-tight">
            {t('levels.title')}
          </h1>
          <p className="max-w-prose text-muted-foreground">{t('levels.description')}</p>
        </div>
        <Button size="sm" onClick={() => setSheet({ mode: 'add' })}>
          <Plus aria-hidden="true" />
          {t('levels.add')}
        </Button>
      </div>

      <DataTable
        label={t('levels.table')}
        empty={t('levels.empty')}
        columns={tableColumns}
        data={rows}
        getRowId={(l) => l.id}
        isError={levels.isError}
        onRetry={() => void levels.refetch()}
        actions={(level) => (
          <RowActions
            editLabel={t('levels.editLabel', { name: level.name })}
            deleteLabel={t('levels.deleteLabel', { name: level.name })}
            onEdit={() => setSheet({ mode: 'edit', level })}
            onDelete={() => setDeleting(level)}
          />
        )}
      />

      <LevelSheet target={sheet} onClose={() => setSheet(null)} />
      <DeleteDialog
        target={deleting}
        id={(l) => l.id}
        mutation={deleteLevelMutation}
        texts={({ name }) => ({
          title: t('levels.deleteTitle', { name }),
          description: t('levels.deleteDescription'),
          inUse: t('levels.inUse', { name }),
          notFound: t('levels.notFound'),
          deleted: t('levels.deleted', { name }),
        })}
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}
