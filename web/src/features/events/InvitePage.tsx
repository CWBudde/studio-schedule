import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { getRouteApi, Link } from '@tanstack/react-router';
import { CalendarCheck, CircleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { eventInviteQuery, publicSettingsQuery, respondToEventInviteMutation } from '@/api/queries';
import { LinkPageCard } from '@/components/LinkPageCard';
import { PendingIcon } from '@/components/PendingIcon';
import { Button } from '@/components/ui/button';
import { CardContent, CardFooter } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { FormError } from '@/features/auth/FormError';
import { statusOf } from '@/features/auth/form';
import { formatDateTime } from '@/lib/schoolTime';
import { EventPage } from './EventPage';

const route = getRouteApi('/events/invite');

export function EventInviteRoute() {
  const { token } = route.useSearch();
  // An event can itself have the slug "invite".
  return token === undefined ? <EventPage slug="invite" /> : <InvitePage token={token} />;
}

export function InvitePage({ token }: { token?: string }) {
  return token ? <Invite key={token} token={token} /> : <Unavailable />;
}

function Unavailable() {
  const { t } = useTranslation('events');
  return (
    <LinkPageCard icon={<CircleAlert />} tone="destructive" title={t('invite.unavailable')}>
      <CardFooter>
        <Button variant="outline" asChild>
          <Link to="/events">{t('title')}</Link>
        </Button>
      </CardFooter>
    </LinkPageCard>
  );
}

function Invite({ token }: { token: string }) {
  const { t, i18n } = useTranslation('events');
  const invite = useQuery(eventInviteQuery(token));
  const settings = useQuery(publicSettingsQuery);
  const write = useMutation(respondToEventInviteMutation(useQueryClient()));
  const lang = i18n.resolvedLanguage ?? 'en';

  if (statusOf(invite.error) === 404 || statusOf(write.error) === 404) return <Unavailable />;
  if (write.isSuccess) {
    return (
      <LinkPageCard
        icon={<CalendarCheck />}
        title={t(write.variables.accept ? 'invite.accepted' : 'invite.declined')}
        description={invite.data?.event.title}
        status
      >
        <CardFooter>
          <Button asChild>
            <Link to="/events">{t('title')}</Link>
          </Button>
        </CardFooter>
      </LinkPageCard>
    );
  }

  const error = invite.error ?? settings.error;
  return (
    <LinkPageCard title={t('invite.title')}>
      <CardContent className="space-y-4">
        {error ? (
          <>
            <FormError error={error} />
            <Button
              variant="outline"
              onClick={() => {
                if (invite.isError) void invite.refetch();
                if (settings.isError) void settings.refetch();
              }}
            >
              {t('retry')}
            </Button>
          </>
        ) : invite.data && settings.data ? (
          <>
            <h2 className="font-heading text-lg font-semibold">{invite.data.event.title}</h2>
            <p>
              {t('invite.reserved', {
                name: invite.data.givenName,
                at: formatDateTime(invite.data.expiresAt, settings.data.timezone, lang),
              })}
            </p>
            {write.error && <FormError error={write.error} />}
            <div className="flex flex-wrap gap-2">
              <Button
                requiresOnline
                disabled={write.isPending}
                onClick={() => write.mutate({ token, accept: true })}
              >
                <PendingIcon pending={write.isPending && write.variables?.accept === true} />
                {t('accept')}
              </Button>
              <Button
                requiresOnline
                variant="outline"
                disabled={write.isPending}
                onClick={() => write.mutate({ token, accept: false })}
              >
                <PendingIcon pending={write.isPending && !write.variables?.accept} />
                {t('decline')}
              </Button>
            </div>
          </>
        ) : (
          <Skeleton className="h-24" aria-busy="true" />
        )}
      </CardContent>
    </LinkPageCard>
  );
}
