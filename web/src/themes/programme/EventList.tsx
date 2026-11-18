import { Link } from '@tanstack/react-router';
import { CalendarDays } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import type { EventListProps } from '../slots';

export function EventList({ events }: EventListProps) {
  const { t } = useTranslation('events');
  if (!events.length) return <p className="text-muted-foreground">{t('empty')}</p>;
  return (
    <ul className="grid gap-4 md:grid-cols-2">
      {events.map((event) => (
        <li key={event.slug}>
          <Link
            to="/events/$slug"
            params={{ slug: event.slug }}
            className="block h-full space-y-3 rounded-xl border bg-card p-5 text-card-foreground hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
          >
            <h2 className="font-heading text-xl font-semibold">{event.title}</h2>
            {event.nextSession && (
              <p className="flex items-baseline gap-2 text-sm text-muted-foreground">
                <CalendarDays className="size-4 shrink-0 self-center" aria-hidden />
                <span>
                  {event.nextSession.date} · {event.nextSession.time}
                </span>
              </p>
            )}
            <p className="flex flex-wrap items-baseline gap-3">
              <span className="font-medium">{event.price}</span>
              <Badge variant="secondary">{event.availability}</Badge>
            </p>
          </Link>
        </li>
      ))}
    </ul>
  );
}
