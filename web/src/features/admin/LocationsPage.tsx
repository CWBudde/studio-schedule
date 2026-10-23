import { useQuery } from '@tanstack/react-query';
import { MapPinPlus, Plus } from 'lucide-react';
import { type ReactNode, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
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
import { DeleteDialog } from './crud/DeleteDialog';
import { DataTable } from './crud/DataTable';
import { RowActions } from './crud/RowActions';
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
      <DeleteDialog
        target={deleting?.kind === 'location' ? deleting.location : null}
        id={(l) => l.id}
        mutation={deleteLocationMutation}
        texts={({ name }) => ({
          title: t('locations.deleteTitle', { name }),
          description: t('locations.deleteDescription'),
          inUse: t('locations.inUse', { name }),
          notFound: t('locations.notFound'),
          deleted: t('locations.deleted', { name }),
        })}
        onClose={() => setDeleting(null)}
      />
      <DeleteDialog
        target={deleting?.kind === 'room' ? deleting.room : null}
        id={(r) => r.id}
        mutation={deleteRoomMutation}
        texts={({ name, locationName }) => ({
          title: t('rooms.deleteTitle', { name }),
          description: t('rooms.deleteDescription', { location: locationName }),
          inUse: t('rooms.inUse', { name }),
          notFound: t('rooms.notFound'),
          deleted: t('rooms.deleted', { name }),
        })}
        onClose={() => setDeleting(null)}
      />
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
