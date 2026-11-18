import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from '@tanstack/react-router';
import { ArrowLeft } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ApiError, adminEventQuery, publicSettingsQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { LoadError } from '../crud/LoadError';
import { EventActions } from './EventActions';
import { EventImageCard } from './EventImageCard';
import { EventRegistrations } from './EventRegistrations';
import { EventSessions } from './EventSessions';

export function EventRoute() {
  const { eventId } = useParams({ from: '/admin/events/$eventId' });
  return <EventPage key={eventId} eventId={eventId} />;
}

export function EventPage({ eventId }: { eventId: string }) {
  const { t } = useTranslation('adminEvents');
  const event = useQuery(adminEventQuery(eventId));
  const settings = useQuery(publicSettingsQuery);
  const navigate = useNavigate();
  const notFound = event.error instanceof ApiError && event.error.status === 404;
  const current = event.data;

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" asChild>
        <Link to="/admin/events">
          <ArrowLeft aria-hidden="true" />
          {t('title')}
        </Link>
      </Button>
      {notFound ? (
        <p role="alert">{t('notFound')}</p>
      ) : event.isError || settings.isError ? (
        <LoadError
          message={t('loadError')}
          onRetry={() => {
            if (event.isError) void event.refetch();
            if (settings.isError) void settings.refetch();
          }}
        />
      ) : !current || !settings.data ? (
        <Skeleton className="h-28" />
      ) : (
        <>
          <header className="space-y-3">
            <div className="flex flex-wrap items-center gap-3">
              <h1 className="font-heading text-3xl font-semibold tracking-tight">
                {current.title}
              </h1>
              <Badge variant="secondary">{t(`eventStatus.${current.status}`)}</Badge>
            </div>
            {current.description && (
              <p className="max-w-prose whitespace-pre-wrap text-muted-foreground">
                {current.description}
              </p>
            )}
            <p className="text-sm text-muted-foreground">
              {t('occupancyLabel', {
                taken: current.registered + current.invited,
                capacity: current.capacity,
              })}
            </p>
            <EventActions
              event={current}
              onDeleted={() => void navigate({ to: '/admin/events' })}
            />
          </header>
          <EventSessions event={current} timeZone={settings.data.timezone} />
          <EventImageCard event={current} />
          <EventRegistrations event={current} />
        </>
      )}
    </div>
  );
}
