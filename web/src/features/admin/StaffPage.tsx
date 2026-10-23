import { useQuery } from '@tanstack/react-query';
import { UserPlus } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  allStaffQuery,
  deactivateStaffMutation,
  deleteStaffMutation,
  type StaffEntry,
} from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { DeleteDialog } from './crud/DeleteDialog';
import { DataTable } from './crud/DataTable';
import { personName } from './crud/person';
import { RowActions } from './crud/RowActions';
import { columnHelper } from './crud/table';
import { StaffSheet, type StaffTarget } from './StaffSheet';

type Shown = 'all' | 'active' | 'inactive';

const columns = columnHelper<StaffEntry>();

/**
 * The instructors and accompanists of the school (/admin/staff), for the
 * office: add them, edit their name, role and person, deactivate former
 * staff (who leave the public lists but stay on their lessons) and delete
 * the ones nothing refers to.
 */
export function StaffPage() {
  const { t } = useTranslation('admin');
  const staff = useQuery(allStaffQuery);
  const [shown, setShown] = useState<Shown>('all');
  const [sheet, setSheet] = useState<StaffTarget | null>(null);
  const [deleting, setDeleting] = useState<StaffEntry | null>(null);

  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor('displayName', {
          header: t('staff.displayName'),
          cell: (info) => (
            <span
              className={
                info.row.original.active ? 'font-medium' : 'font-medium text-muted-foreground'
              }
            >
              {info.getValue()}
            </span>
          ),
        }),
        columns.accessor((s) => t(`staff.kinds.${s.kind}`), {
          id: 'kind',
          header: t('staff.kind'),
        }),
        columns.accessor((s) => (s.person ? personName(s.person) : undefined), {
          id: 'person',
          header: t('staff.person'),
          sortUndefined: 'last',
        }),
        columns.accessor((s) => (s.active ? t('staff.active') : t('staff.inactive')), {
          id: 'status',
          header: t('staff.status'),
          cell: (info) => (
            <Badge variant={info.row.original.active ? 'secondary' : 'outline'}>
              {info.getValue()}
            </Badge>
          ),
        }),
      ]),
    [t],
  );

  const rows = useMemo(
    () =>
      shown === 'all' ? staff.data : staff.data?.filter((s) => s.active === (shown === 'active')),
    [staff.data, shown],
  );
  const empty =
    staff.data?.length === 0
      ? t('staff.empty')
      : shown === 'active'
        ? t('staff.noActive')
        : t('staff.noInactive');

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('staff.title')}</h1>
          <p className="max-w-prose text-muted-foreground">{t('staff.description')}</p>
        </div>
        <Button size="sm" onClick={() => setSheet({ mode: 'add' })}>
          <UserPlus aria-hidden="true" />
          {t('staff.add')}
        </Button>
      </div>

      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        spacing={0}
        value={shown}
        onValueChange={(value) => {
          if (value === 'all' || value === 'active' || value === 'inactive') setShown(value);
        }}
        aria-label={t('staff.show')}
      >
        <ToggleGroupItem value="all">{t('staff.showAll')}</ToggleGroupItem>
        <ToggleGroupItem value="active">{t('staff.showActive')}</ToggleGroupItem>
        <ToggleGroupItem value="inactive">{t('staff.showInactive')}</ToggleGroupItem>
      </ToggleGroup>

      <DataTable
        label={t('staff.table')}
        searchLabel={t('staff.search')}
        empty={empty}
        columns={tableColumns}
        data={rows}
        getRowId={(s) => s.id}
        isError={staff.isError}
        onRetry={() => void staff.refetch()}
        initialSorting={[{ id: 'displayName', desc: false }]}
        actions={(member) => (
          <RowActions
            editLabel={t('staff.editLabel', { name: member.displayName })}
            deleteLabel={t('staff.deleteLabel', { name: member.displayName })}
            onEdit={() => setSheet({ mode: 'edit', staff: member })}
            onDelete={() => setDeleting(member)}
          />
        )}
      />

      <StaffSheet target={sheet} onClose={() => setSheet(null)} />
      <DeleteDialog
        target={deleting}
        id={(s) => s.id}
        mutation={deleteStaffMutation}
        texts={({ displayName: name, active }) => ({
          title: t('staff.deleteTitle', { name }),
          description: active ? t('staff.deleteDescription') : t('staff.deleteDescriptionInactive'),
          // Suggests deactivating a staff member who still teaches.
          inUse: active ? t('staff.inUse', { name }) : t('staff.inUseInactive', { name }),
          notFound: t('staff.notFound'),
          deleted: t('staff.deleted', { name }),
        })}
        inUseAction={({ displayName: name, active }) =>
          active
            ? {
                label: t('staff.deactivate'),
                pending: t('staff.deactivating'),
                done: t('staff.deactivated', { name }),
                mutation: deactivateStaffMutation,
              }
            : undefined
        }
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}
