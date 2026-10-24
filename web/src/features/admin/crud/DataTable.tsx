import { type RowData, type SortingState, useTable } from '@tanstack/react-table';
import { ArrowDown, ArrowUp, ArrowUpDown, Search } from 'lucide-react';
import { Fragment, type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { type CrudColumn, crudFeatures } from './table';
import { LoadError } from './LoadError';
import { useIsPhone } from './useIsPhone';

export type DataTableProps<T extends RowData> = {
  /** The accessible name of the table (or of the card list on phones). */
  label: string;
  /**
   * The accessible name of the search box over all columns; without one, the
   * table has no search box (e.g. when the API searches).
   */
  searchLabel?: string;
  /** What to show when there are no rows at all. */
  empty: ReactNode;
  /** Stable column definitions (module scope or memoized). */
  columns: CrudColumn<T>[];
  /** The rows; undefined while they load. */
  data: T[] | undefined;
  getRowId: (row: T) => string;
  /** Whether loading the rows failed. */
  isError?: boolean;
  onRetry?: () => void;
  initialSorting?: SortingState;
  /** The buttons of a row (edit, delete, …). */
  actions?: (row: T) => ReactNode;
};

const noRows: never[] = [];

/**
 * A table of records with sorting by column, a search over all columns, and
 * loading, error, empty and no-match states. On phones the rows are cards.
 */
export function DataTable<T extends RowData>({
  label,
  searchLabel,
  empty,
  columns,
  data,
  getRowId,
  isError = false,
  onRetry,
  initialSorting = noRows,
  actions,
}: DataTableProps<T>) {
  const { t } = useTranslation('admin');
  const phone = useIsPhone();
  const [sorting, setSorting] = useState<SortingState>(initialSorting);
  const [query, setQuery] = useState('');
  const table = useTable({
    features: crudFeatures,
    columns,
    data: data ?? (noRows as T[]),
    getRowId: (row) => getRowId(row),
    state: { sorting, globalFilter: query },
    onSortingChange: setSorting,
    onGlobalFilterChange: setQuery,
    globalFilterFn: 'includesString',
    sortDescFirst: false,
    enableSortingRemoval: false,
  });

  if (data === undefined) {
    return isError ? <LoadError onRetry={onRetry} /> : <DataTableLoading />;
  }
  if (data.length === 0) {
    return (
      <p className="rounded-xl border border-dashed px-4 py-8 text-center text-muted-foreground">
        {empty}
      </p>
    );
  }

  const rows = table.getRowModel().rows;
  const headers = new Map(table.getFlatHeaders().map((h) => [h.column.id, h]));
  return (
    <div className="space-y-3">
      {searchLabel && (
        <div className="relative max-w-xs">
          <Search
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            type="search"
            aria-label={searchLabel}
            placeholder={t('table.search')}
            className="pl-8"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
        </div>
      )}
      {rows.length === 0 ? (
        <p className="rounded-xl border border-dashed px-4 py-8 text-center text-muted-foreground">
          {t('table.noMatches', { query })}
        </p>
      ) : phone ? (
        <ul role="list" aria-label={label} className="space-y-3">
          {rows.map((row) => {
            const [first, ...rest] = row.getAllCells();
            return (
              <li key={row.id} className="space-y-3 rounded-xl p-4 ring-1 ring-foreground/10">
                <h3 className="font-medium">{first && <table.FlexRender cell={first} />}</h3>
                {rest.length > 0 && (
                  <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                    {rest.map((cell) => (
                      <Fragment key={cell.id}>
                        <dt className="text-muted-foreground">
                          {headers.get(cell.column.id) ? (
                            <table.FlexRender header={headers.get(cell.column.id)!} />
                          ) : (
                            cell.column.id
                          )}
                        </dt>
                        <dd className="min-w-0 break-words">
                          <table.FlexRender cell={cell} />
                        </dd>
                      </Fragment>
                    ))}
                  </dl>
                )}
                {actions && (
                  <div className="flex flex-wrap justify-end gap-2">{actions(row.original)}</div>
                )}
              </li>
            );
          })}
        </ul>
      ) : (
        <div className="rounded-xl ring-1 ring-foreground/10">
          <Table aria-label={label}>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => {
                    const sorted = header.column.getIsSorted();
                    const canSort = header.column.getCanSort();
                    return (
                      <TableHead
                        key={header.id}
                        className="first:pl-4"
                        aria-sort={
                          !canSort
                            ? undefined
                            : sorted === 'asc'
                              ? 'ascending'
                              : sorted === 'desc'
                                ? 'descending'
                                : 'none'
                        }
                      >
                        {header.isPlaceholder ? null : canSort ? (
                          <button
                            type="button"
                            className="-mx-1 inline-flex items-center gap-1 rounded-md px-1 py-0.5 hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                            onClick={header.column.getToggleSortingHandler()}
                          >
                            <table.FlexRender header={header} />
                            <SortIcon sorted={sorted} />
                          </button>
                        ) : (
                          <table.FlexRender header={header} />
                        )}
                      </TableHead>
                    );
                  })}
                  {actions && (
                    <TableHead className="pr-4 text-right">{t('table.actions')}</TableHead>
                  )}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.id}>
                  {row.getAllCells().map((cell) => (
                    <TableCell key={cell.id} className="whitespace-normal first:pl-4">
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                  {actions && (
                    <TableCell className="pr-4">
                      <div className="flex justify-end gap-1">{actions(row.original)}</div>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function SortIcon({ sorted }: { sorted: false | 'asc' | 'desc' }) {
  const Icon = sorted === 'asc' ? ArrowUp : sorted === 'desc' ? ArrowDown : ArrowUpDown;
  return (
    <Icon
      className={sorted ? 'size-3.5' : 'size-3.5 text-muted-foreground/60'}
      aria-hidden="true"
    />
  );
}

/** The loading state of a DataTable, for a page that waits for something before its table. */
export function DataTableLoading() {
  const { t } = useTranslation('admin');
  return (
    <div role="status" className="space-y-3">
      <span className="sr-only">{t('loading')}</span>
      {[0, 1, 2].map((row) => (
        <Skeleton key={row} className="h-10 w-full" aria-hidden="true" />
      ))}
    </div>
  );
}
