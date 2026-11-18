import { useQuery } from '@tanstack/react-query';
import { getRouteApi, Link } from '@tanstack/react-router';
import { ArrowLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { eventImageQuery, eventQuery, publicSettingsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { LoadError } from '@/features/admin/crud/LoadError';
import { statusOf } from '@/features/auth/form';
import { useOnline } from '@/lib/online';
import { useDataUrl } from '@/lib/useDataUrl';
import { useSlot } from '@/themes/useSlot';
import { RegisterSection } from './RegisterSection';
import { useEventView } from './useEventView';

const route = getRouteApi('/events/$slug');

export function PublicEventRoute() {
  const { slug } = route.useParams();
  return <EventPage key={slug} slug={slug} />;
}

export function EventPage({ slug }: { slug: string }) {
  const { t } = useTranslation('events');
  const event = useQuery(eventQuery(slug));
  const settings = useQuery(publicSettingsQuery);
  const image = useQuery(eventImageQuery(slug, event.data?.hasImage ?? false));
  const imageUrl = useDataUrl(image.data);
  const view = useEventView(settings.data?.timezone ?? 'UTC');
  const Detail = useSlot('EventDetail');
  const online = useOnline();

  return (
    <div className="mx-auto w-full max-w-3xl space-y-5">
      <Button variant="ghost" asChild>
        <Link to="/events">
          <ArrowLeft aria-hidden />
          {t('title')}
        </Link>
      </Button>
      {event.data && settings.data ? (
        // Remounting the slot on a theme change is intentional.
        // eslint-disable-next-line react-hooks/static-components
        <Detail
          event={view(event.data)}
          image={event.data.hasImage ? imageUrl : undefined}
          registration={
            <RegisterSection key={slug} event={event.data} timeZone={settings.data.timezone} />
          }
        />
      ) : statusOf(event.error) === 404 ? (
        <h1 className="font-heading text-3xl">{t('notFound')}</h1>
      ) : !online ? (
        <p>{t('offline')}</p>
      ) : event.isError || settings.isError ? (
        <LoadError
          message={t('loadError')}
          onRetry={() => {
            if (event.isError) void event.refetch();
            if (settings.isError) void settings.refetch();
          }}
        />
      ) : (
        <Skeleton className="h-44 w-full" aria-busy="true" />
      )}
    </div>
  );
}
