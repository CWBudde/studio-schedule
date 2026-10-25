import { useQuery } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  deletePassTypeMutation,
  type PassType,
  passTypesQuery,
  retirePassTypeMutation,
} from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { passTypeTerms } from '@/features/passes/passText';
import { useMoney } from '@/features/passes/useMoney';
import { formatRate } from '@/lib/money';
import { DataTable } from './crud/DataTable';
import { DeleteDialog } from './crud/DeleteDialog';
import { RowActions } from './crud/RowActions';
import { columnHelper } from './crud/table';
import { PassTypeSheet, type PassTypeTarget } from './PassTypeSheet';

const columns = columnHelper<PassType>();

/**
 * The pass types of the school (/admin/pass-types), for the office: what
 * the school sells, at which price and VAT rate. A type that was never sold
 * can be deleted; a sold one is retired instead (no longer sold, kept for
 * the passes that have it).
 */
export function PassTypesPage() {
  const { t, i18n } = useTranslation('passes');
  const lang = i18n.resolvedLanguage ?? 'en';
  const passTypes = useQuery(passTypesQuery);
  const money = useMoney();
  const { currency, format } = money;
  const [sheet, setSheet] = useState<PassTypeTarget | null>(null);
  const [deleting, setDeleting] = useState<PassType | null>(null);

  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor('name', {
          header: t('types.name'),
          cell: (info) => <span className="font-medium">{info.getValue()}</span>,
        }),
        columns.accessor((p) => passTypeTerms(t, p), {
          id: 'terms',
          header: t('types.terms'),
        }),
        columns.accessor('price', {
          header: t('types.price'),
          cell: (info) => <span className="tabular-nums">{format(info.getValue())}</span>,
        }),
        columns.accessor('vatRate', {
          header: t('types.vatRate'),
          cell: (info) => <span className="tabular-nums">{formatRate(info.getValue(), lang)}</span>,
        }),
        columns.accessor((p) => (p.active ? t('types.onSale') : t('types.retired')), {
          id: 'active',
          header: t('types.status'),
          cell: (info) => (
            <Badge variant={info.row.original.active ? 'secondary' : 'outline'}>
              {info.getValue()}
            </Badge>
          ),
        }),
      ]),
    [t, lang, format],
  );

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('types.title')}</h1>
          <p className="max-w-prose text-muted-foreground">{t('types.description')}</p>
        </div>
        <Button size="sm" disabled={!currency} onClick={() => setSheet({ mode: 'add' })}>
          <Plus aria-hidden="true" />
          {t('types.add')}
        </Button>
      </div>

      <DataTable
        label={t('types.table')}
        searchLabel={t('types.search')}
        empty={t('types.empty')}
        columns={tableColumns}
        data={currency ? passTypes.data : undefined}
        getRowId={(p) => p.id}
        isError={passTypes.isError || money.isError}
        onRetry={() => {
          if (passTypes.isError) void passTypes.refetch();
          if (money.isError) money.retry();
        }}
        initialSorting={[{ id: 'name', desc: false }]}
        actions={(passType) => (
          <RowActions
            editLabel={t('types.editLabel', { name: passType.name })}
            deleteLabel={t('types.deleteLabel', { name: passType.name })}
            onEdit={() => setSheet({ mode: 'edit', passType })}
            onDelete={() => setDeleting(passType)}
          />
        )}
      />

      {currency && (
        <PassTypeSheet target={sheet} currency={currency} onClose={() => setSheet(null)} />
      )}
      <DeleteDialog
        target={deleting}
        id={(p) => p.id}
        mutation={deletePassTypeMutation}
        texts={({ name, active }) => ({
          title: t('types.deleteTitle', { name }),
          description: t('types.deleteDescription'),
          inUse: active ? t('types.inUse', { name }) : t('types.inUseRetired', { name }),
          notFound: t('types.notFound'),
          deleted: t('types.deleted', { name }),
        })}
        inUseAction={(passType) =>
          passType.active
            ? {
                label: t('types.retire'),
                pending: t('types.retiring'),
                done: t('types.retiredToast', { name: passType.name }),
                mutation: retirePassTypeMutation,
              }
            : undefined
        }
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}
