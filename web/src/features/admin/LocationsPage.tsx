import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { MapPinPlus, Pencil, Plus, Trash2 } from 'lucide-react';
import { type ReactNode, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  deleteLocationMutation,
  deleteRoomMutation,
  type Location,
  locationsQuery,
  type Room,
  roomsQuery,
} from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { ConfirmDelete } from './crud/ConfirmDelete';
import { DataTable } from './crud/DataTable';
import { columnHelper } from './crud/table';
import { LocationSheet, type LocationTarget } from './LocationSheet';
import { RoomSheet, type RoomTarget } from './RoomSheet';

/** `roomCount` is unknown (undefined) until the rooms have loaded. */
type LocationRow = Location & { roomCount: number | undefined };
type RoomRow = Room & { locationName: string };

type DeleteTarget = { kind: 'location'; location: Location } | { kind: 'room'; room: RoomRow };

const locationColumns = columnHelper<LocationRow>();
const roomColumns = columnHelper<RoomRow>();

/**
 * The locations of the school and their rooms (/admin/locations), for the
 * office: add, rename and delete them. The API refuses to delete what is
 * still used, and the page says why.
 */
export function LocationsPage() {
  const { t } = useTranslation('admin');
  const locations = useQuery(locationsQuery);
  const rooms = useQuery(roomsQuery);
  const [locationSheet, setLocationSheet] = useState<LocationTarget | null>(null);
  const [roomSheet, setRoomSheet] = useState<RoomTarget | null>(null);

  // Not "no rooms" while unknown: that would invite deleting a location with rooms.
  const roomsFailed = rooms.data === undefined && rooms.isError;
  const locationRows = useMemo<LocationRow[] | undefined>(
    () =>
      locations.data?.map((location) => ({
        ...location,
        roomCount: rooms.data?.filter((r) => r.locationId === location.id).length,
      })),
    [locations.data, rooms.data],
  );
  const roomRows = useMemo<RoomRow[] | undefined>(() => {
    if (!rooms.data || !locations.data) return undefined;
    const names = new Map(locations.data.map((l) => [l.id, l.name]));
    return rooms.data.map((room) => ({ ...room, locationName: names.get(room.locationId) ?? '' }));
  }, [rooms.data, locations.data]);

  const locationTableColumns = useMemo(
    () =>
      locationColumns.columns([
        locationColumns.accessor('name', {
          header: t('locations.name'),
          cell: (info) => <span className="font-medium">{info.getValue()}</span>,
        }),
        locationColumns.accessor('address', {
          header: t('locations.address'),
          sortUndefined: 'last',
          cell: (info) => (
            <span className="whitespace-pre-line text-muted-foreground">{info.getValue()}</span>
          ),
        }),
        locationColumns.accessor('roomCount', {
          header: t('locations.rooms'),
          sortUndefined: 'last',
          cell: (info) => <RoomCount count={info.getValue()} failed={roomsFailed} />,
        }),
      ]),
    [t, roomsFailed],
  );
  const roomTableColumns = useMemo(
    () =>
      roomColumns.columns([
        roomColumns.accessor('name', {
          header: t('rooms.name'),
          cell: (info) => <span className="font-medium">{info.getValue()}</span>,
        }),
        roomColumns.accessor('locationName', { header: t('rooms.location') }),
        roomColumns.accessor('capacity', { header: t('rooms.capacity') }),
      ]),
    [t],
  );

  const [deleting, setDeleting] = useState<DeleteTarget | null>(null);

  return (
    <div className="space-y-10">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">
          {t('locations.title')}
        </h1>
        <p className="max-w-prose text-muted-foreground">{t('locations.description')}</p>
      </div>

      <Section
        title={t('locations.heading')}
        action={
          <Button size="sm" onClick={() => setLocationSheet({ mode: 'add' })}>
            <MapPinPlus aria-hidden="true" />
            {t('locations.add')}
          </Button>
        }
      >
        <DataTable
          label={t('locations.table')}
          searchLabel={t('locations.search')}
          empty={t('locations.empty')}
          columns={locationTableColumns}
          data={locationRows}
          getRowId={(l) => l.id}
          isError={locations.isError}
          onRetry={() => void locations.refetch()}
          initialSorting={[{ id: 'name', desc: false }]}
          actions={(location) => (
            <RowActions
              editLabel={t('locations.editLabel', { name: location.name })}
              deleteLabel={t('locations.deleteLabel', { name: location.name })}
              onEdit={() => setLocationSheet({ mode: 'edit', location })}
              onDelete={() => setDeleting({ kind: 'location', location })}
            />
          )}
        />
      </Section>

      <Section
        title={t('rooms.heading')}
        action={
          <Button
            size="sm"
            disabled={!locations.data?.length}
            onClick={() => setRoomSheet({ mode: 'add' })}
          >
            <Plus aria-hidden="true" />
            {t('rooms.add')}
          </Button>
        }
      >
        <DataTable
          label={t('rooms.table')}
          searchLabel={t('rooms.search')}
          empty={locations.data?.length === 0 ? t('rooms.needLocation') : t('rooms.empty')}
          columns={roomTableColumns}
          data={roomRows}
          getRowId={(r) => r.id}
          isError={rooms.isError || locations.isError}
          onRetry={() => {
            if (rooms.isError) void rooms.refetch();
            if (locations.isError) void locations.refetch();
          }}
          initialSorting={[
            { id: 'locationName', desc: false },
            { id: 'name', desc: false },
          ]}
          actions={(room) => (
            <RowActions
              editLabel={t('rooms.editLabel', { name: room.name, location: room.locationName })}
              deleteLabel={t('rooms.deleteLabel', {
                name: room.name,
                location: room.locationName,
              })}
              onEdit={() => setRoomSheet({ mode: 'edit', room })}
              onDelete={() => setDeleting({ kind: 'room', room })}
            />
          )}
        />
      </Section>

      <LocationSheet target={locationSheet} onClose={() => setLocationSheet(null)} />
      <RoomSheet
        target={roomSheet}
        locations={locations.data ?? []}
        onClose={() => setRoomSheet(null)}
      />
      <DeleteDialog target={deleting} onClose={() => setDeleting(null)} />
    </div>
  );
}

/** The number of rooms of a location; a placeholder until the rooms are known. */
function RoomCount({ count, failed }: { count: number | undefined; failed: boolean }) {
  const { t } = useTranslation('admin');
  if (count !== undefined) return t('locations.roomCount', { count });
  if (failed)
    return <span className="text-muted-foreground">{t('locations.roomCountUnknown')}</span>;
  return (
    <span>
      <span className="sr-only">{t('loading')}</span>
      <Skeleton className="h-4 w-16" aria-hidden="true" />
    </span>
  );
}

function Section({
  title,
  action,
  children,
}: {
  title: string;
  action: ReactNode;
  children: ReactNode;
}) {
  const headingId = useId();
  return (
    <section aria-labelledby={headingId} className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 id={headingId} className="font-heading text-xl font-semibold">
          {title}
        </h2>
        {action}
      </div>
      {children}
    </section>
  );
}

function RowActions({
  editLabel,
  deleteLabel,
  onEdit,
  onDelete,
}: {
  editLabel: string;
  deleteLabel: string;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation('admin');
  return (
    <>
      <Button variant="outline" size="sm" aria-label={editLabel} onClick={onEdit}>
        <Pencil aria-hidden="true" />
        <span className="hidden md:inline">{t('form.edit')}</span>
      </Button>
      <Button variant="outline" size="sm" aria-label={deleteLabel} onClick={onDelete}>
        <Trash2 aria-hidden="true" />
        <span className="hidden md:inline">{t('form.delete')}</span>
      </Button>
    </>
  );
}

/** Asks before deleting a location or a room; says why when the API refuses. */
function DeleteDialog({ target, onClose }: { target: DeleteTarget | null; onClose: () => void }) {
  // Keep the texts while the dialog animates out.
  const [shown, setShown] = useState(target);
  if (target !== null && target !== shown) setShown(target);
  if (!shown) return null;
  return (
    <DeleteConfirm
      // A fresh mutation (without the last error) for every record.
      key={shown.kind === 'location' ? shown.location.id : shown.room.id}
      target={shown}
      open={target !== null}
      onClose={onClose}
    />
  );
}

function DeleteConfirm({
  target,
  open,
  onClose,
}: {
  target: DeleteTarget;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const deleteLocation = useMutation(deleteLocationMutation(queryClient));
  const deleteRoom = useMutation(deleteRoomMutation(queryClient));
  const mutation = target.kind === 'location' ? deleteLocation : deleteRoom;
  const name = target.kind === 'location' ? target.location.name : target.room.name;
  const onSuccess = () => {
    toast.success(t(target.kind === 'location' ? 'locations.deleted' : 'rooms.deleted', { name }));
    onClose();
  };
  const common = {
    open,
    pending: mutation.isPending,
    error: mutation.error,
    onCancel: onClose,
  };

  return target.kind === 'location' ? (
    <ConfirmDelete
      {...common}
      title={t('locations.deleteTitle', { name })}
      description={t('locations.deleteDescription')}
      conflicts={{ 'in-use': t('locations.inUse', { name }) }}
      messages={{ 404: t('locations.notFound') }}
      onConfirm={() => deleteLocation.mutate(target.location.id, { onSuccess })}
    />
  ) : (
    <ConfirmDelete
      {...common}
      title={t('rooms.deleteTitle', { name })}
      description={t('rooms.deleteDescription', { location: target.room.locationName })}
      conflicts={{ 'in-use': t('rooms.inUse', { name }) }}
      messages={{ 404: t('rooms.notFound') }}
      onConfirm={() => deleteRoom.mutate(target.room.id, { onSuccess })}
    />
  );
}
