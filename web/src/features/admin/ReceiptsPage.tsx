import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { ChevronLeft, ChevronRight, FileText, Search } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type Receipt, receiptsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { formatDate } from '@/lib/schoolTime';
import { formatMoney } from '@/lib/money';
import { DataTable } from './crud/DataTable';
import { SelectField } from './crud/fields';
import { columnHelper } from './crud/table';
import { useDebouncedValue } from './crud/useDebouncedValue';
import { ReceiptKindBadge, ReceiptSheet } from './ReceiptSheet';
import { validateReceiptsSearch } from './receiptsSearch';

const columns = columnHelper<Receipt>();

/** Receipts per page. */
export const receiptsPageSize = 50;

/**
 * The receipts of the school (/admin/receipts), for the office: newest
 * first, by year (of those with receipts) and searched by number or recipient (the API's), a page
 * of 50 at a time. A receipt opens in a sheet (kept in the URL as
 * `receipt`, so a person's pass can link to it) with its PDF and its
 * cancellation.
 */
export function ReceiptsPage() {
  const { t, i18n } = useTranslation('receipts');
  const lang = i18n.resolvedLanguage ?? 'en';
  const navigate = useNavigate();
  const { receipt: openId } = validateReceiptsSearch(useSearch({ strict: false }));
  const [year, setYear] = useState('all');
  const [query, setQuery] = useState('');
  const q = useDebouncedValue(query.trim(), 250);
  const [page, setPage] = useState(0);
  const params = {
    ...(year === 'all' ? {} : { year: Number(year) }),
    ...(q ? { query: q } : {}),
    offset: page * receiptsPageSize,
    limit: receiptsPageSize,
  };
  const receipts = useQuery({ ...receiptsQuery(params), placeholderData: keepPreviousData });
  const total = receipts.data?.total ?? 0;
  const open = (receipt: string | undefined) =>
    void navigate({ to: '/admin/receipts', search: { receipt }, replace: receipt === undefined });

  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor('number', {
          header: t('list.number'),
          enableSorting: false,
          cell: (info) => <span className="font-medium tabular-nums">{info.getValue()}</span>,
        }),
        columns.accessor('issueDate', {
          header: t('list.date'),
          enableSorting: false,
          cell: (info) => (
            <span className="tabular-nums">
              {formatDate(info.getValue(), lang, {
                day: 'numeric',
                month: 'short',
                year: 'numeric',
              })}
            </span>
          ),
        }),
        columns.accessor('recipientName', { header: t('list.recipient'), enableSorting: false }),
        columns.accessor('kind', {
          header: t('list.kind'),
          enableSorting: false,
          cell: (info) => <ReceiptKindBadge receipt={info.row.original} />,
        }),
        columns.accessor('paymentMethod', {
          header: t('method.label'),
          enableSorting: false,
          cell: (info) => t(`method.${info.getValue()}`),
        }),
        columns.accessor('gross', {
          header: t('list.gross'),
          enableSorting: false,
          cell: (info) => (
            <span className="tabular-nums">
              {formatMoney(info.getValue(), info.row.original.currency, lang)}
            </span>
          ),
        }),
      ]),
    [t, lang],
  );

  // The years with receipts, as the API knows them whatever the filter.
  const years = (receipts.data?.years ?? []).map(String);
  const from = total === 0 ? 0 : page * receiptsPageSize + 1;
  const to = Math.min(total, (page + 1) * receiptsPageSize);

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('list.title')}</h1>
        <p className="max-w-prose text-muted-foreground">{t('list.description')}</p>
      </div>

      <div className="flex flex-wrap items-end gap-4">
        <div className="w-40">
          <SelectField
            label={t('list.year')}
            placeholder={t('list.year')}
            options={[
              { value: 'all', label: t('list.allYears') },
              ...years.map((y) => ({ value: y, label: y })),
            ]}
            value={year}
            onChange={(value) => {
              setYear(value);
              setPage(0);
            }}
          />
        </div>
        <div className="relative w-full max-w-sm">
          <Search
            className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            type="search"
            aria-label={t('list.search')}
            placeholder={t('list.searchPlaceholder')}
            className="pl-8"
            maxLength={100}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setPage(0);
            }}
          />
        </div>
      </div>

      <div className="space-y-3">
        <DataTable
          label={t('list.table')}
          empty={q || year !== 'all' ? t('list.noMatches') : t('list.empty')}
          columns={tableColumns}
          data={receipts.isError ? undefined : receipts.data?.items}
          getRowId={(r) => r.id}
          isError={receipts.isError}
          onRetry={() => void receipts.refetch()}
          actions={(r) => (
            <Button
              variant="outline"
              size="sm"
              aria-label={t('list.openLabel', { number: r.number })}
              onClick={() => open(r.id)}
            >
              <FileText aria-hidden="true" />
              <span className="hidden md:inline">{t('list.open')}</span>
            </Button>
          )}
        />
        {total > receiptsPageSize && (
          <nav
            aria-label={t('list.pages')}
            className="flex flex-wrap items-center justify-between gap-2 text-sm"
          >
            <span className="text-muted-foreground tabular-nums">
              {t('list.range', { from, to, total })}
            </span>
            <span className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={page === 0}
                onClick={() => setPage((p) => p - 1)}
              >
                <ChevronLeft aria-hidden="true" />
                {t('list.previous')}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={to >= total}
                onClick={() => setPage((p) => p + 1)}
              >
                {t('list.next')}
                <ChevronRight aria-hidden="true" />
              </Button>
            </span>
          </nav>
        )}
      </div>

      <ReceiptSheet receiptId={openId} onOpen={open} onClose={() => open(undefined)} />
    </div>
  );
}
