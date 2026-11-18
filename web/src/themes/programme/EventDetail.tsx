import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import type { EventDetailProps } from '../slots';

export function EventDetail({ event, image, registration }: EventDetailProps) {
  const { t } = useTranslation('events');
  return (
    <article className="space-y-6">
      <header className="space-y-3">
        <h1 className="font-heading text-4xl font-medium tracking-tight">{event.title}</h1>
        <p className="text-muted-foreground">{event.courseType}</p>
        <p className="flex flex-wrap items-baseline gap-3">
          <strong className="text-xl">{event.price}</strong>
          <Badge variant="secondary">{event.availability}</Badge>
        </p>
      </header>
      {image && <img src={image} alt="" className="max-h-80 w-full rounded-xl object-contain" />}
      {event.description && <p className="max-w-prose whitespace-pre-wrap">{event.description}</p>}
      <section className="space-y-3">
        <h2 className="font-heading text-2xl font-medium">{t('sessions')}</h2>
        <ul className="divide-y rounded-xl border px-4">
          {event.sessions.map((session) => (
            <li key={session.id} className="space-y-0.5 py-3">
              <p className="font-medium">
                {session.date} · {session.time}
              </p>
              <p className="text-muted-foreground">{session.place}</p>
              {session.teachers && <p className="text-muted-foreground">{session.teachers}</p>}
              {session.canceled && <Badge variant="destructive">{t('canceled')}</Badge>}
            </li>
          ))}
        </ul>
      </section>
      {registration}
    </article>
  );
}
