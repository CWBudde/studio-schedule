import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarPlus } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type AdminEvent, adminEventsQuery, publicSettingsQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { formatTimestamp } from '@/lib/schoolTime';
import { DataTable } from '../crud/DataTable';
import { columnHelper } from '../crud/table';
import { EventActions } from './EventActions';
import { EventSheet } from './EventSheet';

const columns = columnHelper<AdminEvent>();

export function EventsPage() {
  const { t, i18n } = useTranslation('adminEvents');
  const language = i18n.resolvedLanguage ?? 'en';
  const events = useQuery(adminEventsQuery);
  const settings = useQuery(publicSettingsQuery);
  const timeZone = settings.data?.timezone;
  const [adding, setAdding] = useState(false);
  const tableColumns = useMemo(
    () =>
      columns.columns([
        columns.accessor('title', {
          header: t('form.title'),
          cell: (cell) => (
            <Link
              to="/admin/events/$eventId"
              params={{ eventId: cell.row.original.id }}
              className="font-medium underline-offset-4 hover:underline"
            >
              {cell.getValue()}
            </Link>
          ),
        }),
        columns.accessor('status', {
          header: t('status'),
          cell: (cell) => <Badge variant="secondary">{t(`eventStatus.${cell.getValue()}`)}</Badge>,
        }),
        columns.accessor((event) => event.sessions[0]?.startsAt ?? '', {
          id: 'firstSession',
          header: t('firstSession'),
          cell: (cell) =>
            cell.getValue() && timeZone
              ? formatTimestamp(cell.getValue(), timeZone, language)
              : '—',
        }),
        columns.accessor((event) => event.registered + event.invited, {
          id: 'occupancy',
          header: t('places'),
          cell: (cell) =>
            t('occupancy', { taken: cell.getValue(), capacity: cell.row.original.capacity }),
        }),
      ]),
    [t, language, timeZone],
  );

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        <Button size="sm" onClick={() => setAdding(true)}>
          <CalendarPlus aria-hidden="true" />
          {t('add')}
        </Button>
      </header>
      <DataTable
        label={t('title')}
        searchLabel={t('search')}
        empty={t('empty')}
        columns={tableColumns}
        data={settings.data ? events.data : undefined}
        getRowId={(event) => event.id}
        isError={events.isError || settings.isError}
        onRetry={() => {
          if (events.isError) void events.refetch();
          if (settings.isError) void settings.refetch();
        }}
        actions={(event) => <EventActions event={event} />}
      />
      <EventSheet target={adding ? { mode: 'add' } : null} onClose={() => setAdding(false)} />
    </div>
  );
}
