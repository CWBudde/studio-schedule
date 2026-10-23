import {
  type ColumnDef,
  columnFilteringFeature,
  createColumnHelper,
  createFilteredRowModel,
  createSortedRowModel,
  filterFn_includesString,
  globalFilteringFeature,
  type RowData,
  rowSortingFeature,
  sortFn_alphanumeric,
  sortFn_basic,
  sortFn_text,
  tableFeatures,
} from '@tanstack/react-table';

/** The table features of every admin table: sorting by a column and a search over all columns. */
export const crudFeatures = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns: { alphanumeric: sortFn_alphanumeric, text: sortFn_text, basic: sortFn_basic },
  columnFilteringFeature,
  globalFilteringFeature,
  filteredRowModel: createFilteredRowModel(),
  filterFns: { includesString: filterFn_includesString },
});

export type CrudFeatures = typeof crudFeatures;

// eslint-disable-next-line @typescript-eslint/no-explicit-any -- columns of different value types
export type CrudColumn<T extends RowData> = ColumnDef<CrudFeatures, T, any>;

/** A column helper for the rows of an admin table. */
export const columnHelper = <T extends RowData>() => createColumnHelper<CrudFeatures, T>();
