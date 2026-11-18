import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import type { EventDetailProps } from '../slots';

export function EventDetail({ event, image, registration }: EventDetailProps) {
  const { t } = useTranslation('events');
  return (
    <article className="werkstatt-event space-y-6">
      <header className="border-b border-primary/25 pb-4">
        <p className="text-muted-foreground">{event.courseType}</p>
        <h1 className="font-heading text-4xl leading-none font-semibold sm:text-5xl">
          {event.title}
        </h1>
        <p className="mt-3 flex flex-wrap items-baseline gap-4 text-lg">
          <strong>{event.price}</strong>
          <span>{event.availability}</span>
        </p>
      </header>
      {image && <img src={image} alt="" className="max-h-80 w-full object-contain" />}
      {event.description && <p className="max-w-prose whitespace-pre-wrap">{event.description}</p>}
      <section className="space-y-3">
        <h2 className="font-heading text-2xl font-medium">{t('sessions')}</h2>
        <ul className="divide-y border-y">
          {event.sessions.map((session) => (
            <li key={session.id} className="grid gap-1 py-3 sm:grid-cols-2 sm:gap-4">
              <p className="font-semibold">
                {session.date}
                <br />
                {session.time}
              </p>
              <div className="text-muted-foreground">
                <p>{session.place}</p>
                {session.teachers && <p>{session.teachers}</p>}
                {session.canceled && <Badge variant="destructive">{t('canceled')}</Badge>}
              </div>
            </li>
          ))}
        </ul>
      </section>
      {registration}
    </article>
  );
}
