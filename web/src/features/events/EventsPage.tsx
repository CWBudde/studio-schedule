import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { eventsQuery, publicSettingsQuery } from '@/api/queries';
import { Skeleton } from '@/components/ui/skeleton';
import { LoadError } from '@/features/admin/crud/LoadError';
import { useOnline } from '@/lib/online';
import { useSlot } from '@/themes/useSlot';
import { useEventView } from './useEventView';

export function EventsPage() {
  const { t } = useTranslation('events');
  const events = useQuery(eventsQuery);
  const settings = useQuery(publicSettingsQuery);
  const view = useEventView(settings.data?.timezone ?? 'UTC');
  const List = useSlot('EventList');
  const online = useOnline();

  return (
    <div className="mx-auto w-full max-w-5xl space-y-6">
      <h1 className="font-heading text-4xl font-medium tracking-tight">{t('title')}</h1>
      {events.data && settings.data ? (
        // Remounting the slot on a theme change is intentional.
        // eslint-disable-next-line react-hooks/static-components
        <List events={events.data.map(view)} />
      ) : !online ? (
        <p>{t('offline')}</p>
      ) : events.isError || settings.isError ? (
        <LoadError
          message={t('loadError')}
          onRetry={() => {
            if (events.isError) void events.refetch();
            if (settings.isError) void settings.refetch();
          }}
        />
      ) : (
        <Skeleton className="h-36 w-full" aria-busy="true" />
      )}
    </div>
  );
}
