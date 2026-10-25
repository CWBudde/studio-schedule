import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { type CalendarFeed, type CalendarFeedSubject, publicSettingsQuery } from '@/api/queries';
import { formatDate, schoolDateOf } from '@/lib/schoolTime';

/**
 * What a "Subscribe" dialog offers a link for (ADR 0011): the family's
 * bookings, the own teaching, or the schedule of every location or of one.
 */
export type FeedSubject =
  | { scope: 'bookings' }
  | { scope: 'teaching' }
  | { scope: 'schedule'; location?: { id: string; name: string } };

/** The body of createCalendarFeed for a subject. */
export function subjectBody(subject: FeedSubject): CalendarFeedSubject {
  return subject.scope === 'schedule' && subject.location
    ? { scope: 'schedule', locationId: subject.location.id }
    : { scope: subject.scope };
}

/** Whether an active feed is the link of a subject (an account has at most one per subject). */
export function isFeedOf(feed: CalendarFeed, subject: FeedSubject): boolean {
  const location = subject.scope === 'schedule' ? subject.location?.id : undefined;
  return feed.scope === subject.scope && feed.locationId === location;
}

/** The name of a feed's subject, e.g. "Your lessons" or "Schedule, North Studio". */
export function useFeedLabel(): (feed: CalendarFeed) => string {
  const { t } = useTranslation('feeds');
  return (feed) =>
    feed.scope === 'schedule' && feed.locationId
      ? t('label.location', { location: feed.locationName ?? '' })
      : t(`label.${feed.scope}`);
}

/**
 * Formats an instant as its school date with the year, e.g. "October 25,
 * 2026"; undefined until the school's time zone is known.
 */
export function useSchoolDay(): (instant: string) => string | undefined {
  const { i18n } = useTranslation();
  const lang = i18n.resolvedLanguage ?? 'en';
  const timeZone = useQuery(publicSettingsQuery).data?.timezone;
  return (instant) =>
    timeZone === undefined
      ? undefined
      : formatDate(schoolDateOf(Date.parse(instant), timeZone), lang, {
          day: 'numeric',
          month: 'long',
          year: 'numeric',
        });
}
