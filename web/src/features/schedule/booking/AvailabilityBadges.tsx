import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { ScheduleLesson } from '@/api/client';
import { meQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { isInvited } from './waitlist';

type Named = { personId: string; name: string };

/**
 * The family's entries on the lesson's waiting list that wait for a place,
 * and those whose invite holds one at `now` (the server's clock).
 */
function waitlistOf(lesson: ScheduleLesson, now: number): { waiting: Named[]; invited: Named[] } {
  const entries = lesson.waitlist ?? [];
  return {
    waiting: entries.filter((w) => w.status === 'waiting'),
    invited: entries.filter((w) => isInvited(w, now)),
  };
}

/** Whether AvailabilityBadges shows anything at `now`, e.g. to refer to it only then. */
export function hasAvailabilityBadges(
  lesson: ScheduleLesson,
  showFree: boolean,
  now: number,
): boolean {
  const { waiting, invited } = waitlistOf(lesson, now);
  return showFree || (lesson.bookedFor ?? []).length > 0 || waiting.length + invited.length > 0;
}

/**
 * The free places of a lesson ("3 free", "Full"), whom of the signed-in
 * family it is booked for ("Booked", "Booked: Mia Sample") and who of them
 * is on its waiting list ("Waiting list") or invited from it ("Invited").
 * The free places are left out for canceled and ended lessons, where they
 * mean nothing, and an invite whose expiry has passed by `now` (the
 * server's clock) shows no more. `id` names the badges, e.g. for the card's aria-describedby
 * (see hasAvailabilityBadges). Until it is known who is signed in, each
 * badge shows without names.
 */
export function AvailabilityBadges({
  id,
  lesson,
  showFree,
  now,
  className,
}: {
  id?: string;
  lesson: ScheduleLesson;
  showFree: boolean;
  now: number;
  className?: string;
}) {
  const { t } = useTranslation('schedule');
  const me = useQuery(meQuery);
  if (!hasAvailabilityBadges(lesson, showFree, now)) return null;
  const booked = lesson.bookedFor ?? [];
  const { waiting, invited } = waitlistOf(lesson, now);

  // The plain text if it is only the signed-in person (or unknown), else the names, "you" first.
  const self = me.data?.personId;
  const textFor = (persons: Named[], plain: string, named: (names: string) => string) => {
    const others = persons.filter((p) => p.personId !== self).map((p) => p.name);
    if (self === undefined || others.length === 0) return plain;
    const selfToo = persons.length > others.length;
    return named([...(selfToo ? [t('booking.badge.you')] : []), ...others].join(', '));
  };

  return (
    <span id={id} className={cn('flex max-w-full flex-wrap gap-1', className)}>
      {booked.length > 0 && (
        <Badge className="max-w-full">
          <span className="truncate">
            {textFor(booked, t('booking.badge.booked'), (names) =>
              t('booking.badge.bookedFor', { names }),
            )}
          </span>
        </Badge>
      )}
      {/* Separates the badges in the description; a flex container does not render it. */}{' '}
      {invited.length > 0 && (
        <Badge className="max-w-full">
          <span className="truncate">
            {textFor(invited, t('booking.badge.invited'), (names) =>
              t('booking.badge.invitedFor', { names }),
            )}
          </span>
        </Badge>
      )}{' '}
      {waiting.length > 0 && (
        <Badge variant="outline" className="max-w-full">
          <span className="truncate">
            {textFor(waiting, t('booking.badge.waiting'), (names) =>
              t('booking.badge.waitingFor', { names }),
            )}
          </span>
        </Badge>
      )}{' '}
      {showFree &&
        (lesson.freePlaces > 0 ? (
          <Badge variant="outline">{t('booking.badge.free', { count: lesson.freePlaces })}</Badge>
        ) : (
          <Badge variant="secondary">{t('booking.badge.full')}</Badge>
        ))}
    </span>
  );
}
