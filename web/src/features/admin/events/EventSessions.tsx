import { useQuery } from '@tanstack/react-query';
import { CalendarPlus } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { LessonSummary } from '@/api/client';
import { type AdminEvent, lessonQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { LessonActions } from '@/features/schedule/admin/LessonActions';
import { LessonFormSheet } from '@/features/schedule/admin/LessonFormSheet';
import { formatTimestamp, formatTime } from '@/lib/schoolTime';
import { LoadError } from '../crud/LoadError';

export function EventSessions({ event, timeZone }: { event: AdminEvent; timeZone: string }) {
  const { t } = useTranslation('adminEvents');
  const [adding, setAdding] = useState(false);
  const canceled = event.status === 'canceled';

  return (
    <Card>
      <CardHeader className="flex flex-wrap items-center justify-between gap-2">
        <CardTitle>
          <h2>{t('sessions.title')}</h2>
        </CardTitle>
        {!canceled && (
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <CalendarPlus aria-hidden="true" />
            {t('sessions.add')}
          </Button>
        )}
      </CardHeader>
      <CardContent>
        {event.sessions.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('sessions.empty')}</p>
        ) : (
          <ul className="divide-y">
            {event.sessions.map((session) => (
              <SessionRow
                key={session.id}
                session={session}
                timeZone={timeZone}
                eventCanceled={canceled}
              />
            ))}
          </ul>
        )}
      </CardContent>
      <LessonFormSheet
        target={adding ? { mode: 'addSession', event } : null}
        onClose={() => setAdding(false)}
      />
    </Card>
  );
}

function SessionRow({
  session,
  timeZone,
  eventCanceled,
}: {
  session: LessonSummary;
  timeZone: string;
  eventCanceled: boolean;
}) {
  const { t, i18n } = useTranslation('adminEvents');
  const language = i18n.resolvedLanguage ?? 'en';
  const detailsId = useId();
  const [expanded, setExpanded] = useState(false);
  const lesson = useQuery({ ...lessonQuery(session.id), enabled: expanded });
  const start = formatTimestamp(session.startsAt, timeZone, language);
  const end = formatTime(session.endsAt, timeZone, language);

  return (
    <li className="space-y-3 py-3 first:pt-0 last:pb-0">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="space-y-1">
          <p className="font-medium">
            <time dateTime={session.startsAt}>{start}</time>
            {' – '}
            <time dateTime={session.endsAt}>{end}</time>
            {session.status === 'canceled' && (
              <Badge variant="secondary" className="ms-2">
                {t('eventStatus.canceled')}
              </Badge>
            )}
          </p>
          <p className="text-muted-foreground">
            {[
              session.location.name,
              session.room.name,
              session.instructor?.displayName,
              session.accompanist?.displayName,
            ]
              .filter(Boolean)
              .join(' · ')}
          </p>
        </div>
        <Button
          size="sm"
          variant="outline"
          aria-expanded={expanded}
          aria-controls={detailsId}
          aria-label={t('sessions.manageDate', { date: start })}
          onClick={() => setExpanded((open) => !open)}
        >
          {t('sessions.manage')}
        </Button>
      </div>
      <div id={detailsId} hidden={!expanded}>
        {expanded &&
          (lesson.isError ? (
            <LoadError message={t('sessions.loadError')} onRetry={() => void lesson.refetch()} />
          ) : lesson.data ? (
            <LessonActions
              lesson={lesson.data}
              timeZone={timeZone}
              eventCanceled={eventCanceled}
              onDeleted={() => setExpanded(false)}
            />
          ) : (
            <Skeleton className="h-16" />
          ))}
      </div>
    </li>
  );
}
