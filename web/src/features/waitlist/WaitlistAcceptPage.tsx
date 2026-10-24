import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearch } from '@tanstack/react-router';
import {
  CalendarCheck,
  CalendarX2,
  CircleAlert,
  CircleCheck,
  Clock,
  Hourglass,
  LinkIcon,
  MapPin,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { closedWhy } from '@/api/conflict';
import {
  acceptWaitlistInviteByTokenMutation,
  publicSettingsQuery,
  type WaitlistInvite,
  waitlistInviteQuery,
} from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { LinkPageCard } from '@/components/LinkPageCard';
import { CardContent, CardFooter } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { FormError } from '@/features/auth/FormError';
import { statusOf } from '@/features/auth/form';
import { useBookingErrorText } from '@/features/schedule/booking/bookingErrors';
import { formatDateTime, formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';

/**
 * The page of the link in a waiting-list invite (/waitlist/accept?token=…),
 * signed in or not: the lesson, whom the place is reserved for and until
 * when, and "Accept the place", which books it. Opening the page only looks
 * at the invite, so a mail scanner that opens the link takes no place. It
 * also says when the invite was used, ran out or ended, when the lesson
 * cannot be booked any more, and when the link is unknown.
 */
export function WaitlistAcceptPage() {
  const { token } = useSearch({ from: '/waitlist/accept' });
  if (!token) return <UnknownLink />;
  return <Invite token={token} />;
}

function Invite({ token }: { token: string }) {
  const { t } = useTranslation('schedule');
  const queryClient = useQueryClient();
  const invite = useQuery(waitlistInviteQuery(token));
  const settings = useQuery(publicSettingsQuery);
  const accept = useMutation(acceptWaitlistInviteByTokenMutation(queryClient));

  if (statusOf(accept.error) === 404) return <UnknownLink />;
  // A loaded invite stays on the page; an error only counts without one.
  if (!invite.data && statusOf(invite.error) === 404) return <UnknownLink />;
  const failed = invite.data ? null : (invite.error ?? settings.error);
  if (failed || (invite.data && settings.error)) {
    const error = failed ?? settings.error;
    return (
      <LinkPageCard icon={<CircleAlert />} title={t('invite.title')} tone="destructive">
        <CardContent className="space-y-4">
          {statusOf(error) === 429 ? (
            <FormError error={error} />
          ) : (
            <Alert variant="destructive">
              <CircleAlert aria-hidden="true" />
              <AlertTitle>{t('invite.loadError')}</AlertTitle>
            </Alert>
          )}
          <Button
            variant="outline"
            onClick={() => {
              if (invite.error) void invite.refetch();
              if (settings.error) void settings.refetch();
            }}
          >
            {t('invite.retry')}
          </Button>
        </CardContent>
      </LinkPageCard>
    );
  }
  if (!invite.data || !settings.data) {
    return (
      <LinkPageCard title={t('invite.title')}>
        <CardContent className="space-y-2" aria-busy="true">
          <p className="sr-only">{t('invite.loading')}</p>
          <Skeleton className="h-5 w-2/3" />
          <Skeleton className="h-5 w-1/2" />
        </CardContent>
      </LinkPageCard>
    );
  }

  return (
    <InviteCard
      invite={invite.data}
      timeZone={settings.data.timezone}
      accepting={accept.isPending}
      error={accept.error}
      onAccept={() => accept.mutate(token)}
    />
  );
}

function InviteCard({
  invite,
  timeZone,
  accepting,
  error,
  onAccept,
}: {
  invite: WaitlistInvite;
  timeZone: string;
  accepting: boolean;
  error: Error | null;
  onAccept: () => void;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const errorText = useBookingErrorText();
  const name = invite.givenName;
  const at = invite.expiresAt && formatDateTime(invite.expiresAt, timeZone, lang);
  const lesson = <LessonFacts invite={invite} timeZone={timeZone} />;

  if (invite.state === 'accepted') {
    return (
      <LinkPageCard icon={<CircleCheck />} title={t('invite.acceptedTitle')}>
        <CardContent className="space-y-4">
          <p role="status">
            {t('invite.accepted', { name })} {t('invite.acceptedMail')}
          </p>
          {lesson}
        </CardContent>
        <ToSchedule />
      </LinkPageCard>
    );
  }
  if (invite.state === 'waiting') {
    // The invite ran out, but the entry went back to the end of the list.
    return (
      <LinkPageCard
        icon={<Hourglass />}
        title={t('invite.waitingTitle')}
        description={
          invite.position === undefined
            ? t('invite.waitingPlain', { name })
            : t('invite.waiting', { name, position: invite.position })
        }
      >
        <CardContent>{lesson}</CardContent>
        <ToSchedule />
      </LinkPageCard>
    );
  }
  if (invite.state === 'expired' || invite.state === 'withdrawn') {
    return (
      <LinkPageCard
        icon={<CalendarX2 />}
        title={t(`invite.${invite.state}Title`)}
        tone="destructive"
        description={
          invite.state === 'expired'
            ? at
              ? t('invite.expired', { name, at })
              : t('invite.expiredPlain', { name })
            : t('invite.withdrawn', { name })
        }
      >
        <CardContent>{lesson}</CardContent>
        <ToSchedule />
      </LinkPageCard>
    );
  }
  if (invite.closed) {
    return (
      <LinkPageCard
        icon={<CalendarX2 />}
        title={t('invite.closedTitle')}
        tone="destructive"
        description={t(`booking.errors.closed.${invite.closed}`)}
      >
        <CardContent>{lesson}</CardContent>
        <ToSchedule />
      </LinkPageCard>
    );
  }

  // A refusal other than closed (which the invite then shows) is said here.
  const refusal = error && !closedWhy(error) && statusOf(error) !== 429 ? error : null;
  return (
    <LinkPageCard
      icon={<CalendarCheck />}
      title={t('invite.title')}
      description={at ? t('invite.reserved', { name, at }) : t('invite.reservedPlain', { name })}
    >
      <CardContent className="space-y-4">
        {lesson}
        <p className="text-sm text-muted-foreground">{t('invite.acceptHint', { name })}</p>
        {statusOf(error) === 429 && <FormError error={error} />}
        {refusal && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{errorText(refusal, name)}</AlertTitle>
          </Alert>
        )}
        <Button className="w-full" disabled={accepting} onClick={onAccept}>
          <CalendarCheck aria-hidden="true" />
          {accepting ? t('invite.accepting') : t('invite.accept')}
        </Button>
      </CardContent>
    </LinkPageCard>
  );
}

/** Course type, school-local day and time, room and location of the invite's lesson. */
function LessonFacts({ invite, timeZone }: { invite: WaitlistInvite; timeZone: string }) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const { lesson } = invite;
  return (
    <div className="space-y-1 rounded-lg border p-3">
      <h2 className="font-medium">{lesson.courseType.name}</h2>
      <p className="flex items-center gap-1.5 text-sm">
        <Clock className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        {t('invite.when', {
          day: formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang),
          time: formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang),
        })}
      </p>
      <p className="flex items-center gap-1.5 text-sm">
        <MapPin className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
        {t('invite.where', { room: lesson.room.name, location: lesson.location.name })}
      </p>
    </div>
  );
}

function ToSchedule() {
  const { t } = useTranslation('schedule');
  return (
    <CardFooter>
      <Button asChild variant="outline">
        <Link to="/schedule">{t('invite.toSchedule')}</Link>
      </Button>
    </CardFooter>
  );
}

function UnknownLink() {
  const { t } = useTranslation('schedule');
  return (
    <LinkPageCard
      icon={<LinkIcon />}
      title={t('invite.unknownTitle')}
      tone="destructive"
      description={t('invite.unknown')}
    >
      <ToSchedule />
    </LinkPageCard>
  );
}
