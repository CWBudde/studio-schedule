import { useTranslation } from 'react-i18next';
import type { PublicEvent } from '@/api/queries';
import { useMoney } from '@/features/passes/useMoney';
import { formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';
import type { EventView } from '@/themes/slots';

export function useEventView(timeZone: string) {
  const { t, i18n } = useTranslation('events');
  const { format } = useMoney();
  const lang = i18n.resolvedLanguage ?? 'en';

  return (e: PublicEvent): EventView => {
    const sessions = e.sessions.map((session) => ({
      id: session.id,
      date: formatDayHeading(schoolDateOf(session.startsAt, timeZone), lang),
      time: formatTimeRange(session.startsAt, session.endsAt, timeZone, lang),
      place: [session.room.name, session.location.name].join(' · '),
      teachers: [session.instructor?.displayName, session.accompanist?.displayName]
        .filter(Boolean)
        .join(', '),
      canceled: session.status === 'canceled',
    }));
    return {
      slug: e.slug,
      title: e.title,
      courseType: e.courseType.name,
      description: e.description,
      price: e.price === 0 ? t('free') : format(e.price),
      availability:
        e.status === 'canceled'
          ? t('canceled')
          : e.freePlaces === 0
            ? t('full')
            : t('places', { count: e.freePlaces }),
      sessions,
      nextSession: sessions.find((session) => !session.canceled) ?? sessions[0],
    };
  };
}
