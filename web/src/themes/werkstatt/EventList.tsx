import { Link } from '@tanstack/react-router';
import { ArrowUpRight } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { EventListProps } from '../slots';

export function EventList({ events }: EventListProps) {
  const { t } = useTranslation('events');
  if (!events.length) return <p className="text-muted-foreground">{t('empty')}</p>;
  return (
    <ul className="werkstatt-events">
      {events.map((event) => (
        <li key={event.slug}>
          <Link to="/events/$slug" params={{ slug: event.slug }} className="werkstatt-events-link">
            {event.nextSession && (
              <p className="text-sm text-muted-foreground">
                {event.nextSession.date} · {event.nextSession.time}
              </p>
            )}
            <h2 className="font-heading text-2xl font-semibold">{event.title}</h2>
            <p className="flex flex-wrap gap-x-4">
              <strong>{event.price}</strong>
              <span className="text-muted-foreground">{event.availability}</span>
            </p>
            <ArrowUpRight className="absolute top-4 right-4 size-5 text-primary" aria-hidden />
          </Link>
        </li>
      ))}
    </ul>
  );
}
