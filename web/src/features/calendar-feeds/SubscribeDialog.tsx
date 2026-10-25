import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import {
  CalendarPlus,
  CircleAlert,
  Copy,
  Download,
  LogIn,
  RefreshCw,
  Unlink,
  UserRoundPlus,
} from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { Me } from '@/api/client';
import { downloadCalendarFeed, subscriptionUrl, webcalUrl } from '@/api/calendarFeedLinks';
import {
  type CalendarFeed,
  calendarFeedsQuery,
  createCalendarFeedMutation,
  meQuery,
  type NewCalendarFeed,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import { LoadError } from '@/features/admin/crud/LoadError';
import { FormError } from '@/features/auth/FormError';
import { RevokeFeed } from './RevokeFeed';
import { type FeedSubject, isFeedOf, subjectBody, useSchoolDay } from './subject';

/**
 * "Subscribe": a button that opens the dialog of a subject's calendar feed
 * (ADR 0011). The dialog says what the feed shows and that its link is
 * secret, whether the account has a link already (created, last used), and
 * creates one (a new one replaces the old) to show it once: to copy, to open
 * in the calendar app and to download. Where no calendar app can reach the
 * feed (the web demo), only the download is offered. Visitors are asked to
 * sign in or register: every feed belongs to an account.
 */
export function SubscribeButton({ subject }: { subject: FeedSubject }) {
  const { t } = useTranslation('feeds');
  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button variant="outline">
          <CalendarPlus aria-hidden="true" />
          {t('subscribe')}
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <SubscribeContent subject={subject} />
      </DialogContent>
    </Dialog>
  );
}

function SubscribeContent({ subject }: { subject: FeedSubject }) {
  const { t } = useTranslation('feeds');
  const me = useQuery(meQuery);
  const location = subject.scope === 'schedule' ? subject.location?.name : undefined;

  return (
    <>
      <DialogHeader className="pr-8">
        <DialogTitle className="font-heading text-lg">
          {location ? t('title.location', { location }) : t(`title.${subject.scope}`)}
        </DialogTitle>
        <DialogDescription>
          {location ? t('content.location', { location }) : t(`content.${subject.scope}`)}
        </DialogDescription>
      </DialogHeader>
      {me.isPending ? (
        <Loading />
      ) : me.isError ? (
        <FormError error={me.error} />
      ) : me.data ? (
        <FeedLink subject={subject} me={me.data} />
      ) : (
        <VisitorPrompt />
      )}
    </>
  );
}

function Loading() {
  const { t } = useTranslation('feeds');
  return (
    <div role="status">
      <span className="sr-only">{t('loading')}</span>
      <Skeleton className="h-24 w-full" aria-hidden="true" />
    </div>
  );
}

/** Decision 2: visitors sign up to subscribe. */
function VisitorPrompt() {
  const { t } = useTranslation('feeds');
  return (
    <div className="space-y-3">
      <p className="font-medium">{t('visitor.title')}</p>
      <p className="text-muted-foreground">{t('visitor.text')}</p>
      <div className="flex flex-wrap gap-2">
        <Button asChild>
          <Link to="/login">
            <LogIn aria-hidden="true" />
            {t('visitor.signIn')}
          </Link>
        </Button>
        <Button asChild variant="outline">
          <Link to="/register">
            <UserRoundPlus aria-hidden="true" />
            {t('visitor.register')}
          </Link>
        </Button>
      </div>
    </div>
  );
}

function FeedLink({ subject, me }: { subject: FeedSubject; me: Me }) {
  const { t } = useTranslation('feeds');
  const queryClient = useQueryClient();
  const feeds = useQuery(calendarFeedsQuery);
  const create = useMutation(createCalendarFeedMutation(queryClient));
  const [created, setCreated] = useState<NewCalendarFeed | null>(null);
  // The feed whose revocation is being confirmed: the confirmation belongs to
  // that feed and goes when it does, however it goes.
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const revokeId = useId();

  const existing = created?.feed ?? feeds.data?.find((feed) => isFeedOf(feed, subject));
  const revoking = existing !== undefined && revokingId === existing.id;
  // Only staff with a staff record of their own have teaching to subscribe to.
  const noTeaching = subject.scope === 'teaching' && !me.teaches;

  return (
    <div className="space-y-4">
      <div className="space-y-2 text-muted-foreground">
        <p>{subject.scope === 'bookings' ? t('window.bookings') : t('window.other')}</p>
        <p>{t('refresh')}</p>
        <p>{t('secret')}</p>
      </div>

      {created ? (
        <CreatedLink created={created} />
      ) : feeds.isPending ? (
        <Loading />
      ) : feeds.isError ? (
        <LoadError message={t('loadFailed')} onRetry={() => void feeds.refetch()} />
      ) : existing ? (
        <ExistingLink feed={existing} />
      ) : (
        <p>{t('none')}</p>
      )}

      <ProblemAlert
        error={create.error}
        messages={{
          ...(subject.scope === 'teaching'
            ? { 403: t('errors.noStaff') }
            : me.impersonator
              ? { 403: t('errors.impersonating') }
              : {}),
          422: t('errors.location'),
        }}
      />

      {me.impersonator ? (
        <Alert>
          <CircleAlert aria-hidden="true" />
          <AlertDescription>{t('errors.impersonating')}</AlertDescription>
        </Alert>
      ) : noTeaching ? (
        <Alert>
          <CircleAlert aria-hidden="true" />
          <AlertDescription>{t('errors.noStaff')}</AlertDescription>
        </Alert>
      ) : (
        !created &&
        feeds.isSuccess && (
          <div className="space-y-2">
            {existing && <p className="text-muted-foreground">{t('regenerateWarning')}</p>}
            <Button
              disabled={create.isPending}
              onClick={() =>
                create.mutate(subjectBody(subject), {
                  onSuccess: (next) => {
                    setRevokingId(null);
                    setCreated(next);
                  },
                })
              }
            >
              {existing ? <RefreshCw aria-hidden="true" /> : <CalendarPlus aria-hidden="true" />}
              {create.isPending ? t('creating') : existing ? t('regenerate') : t('create')}
            </Button>
          </div>
        )
      )}

      {existing && (
        <div className="space-y-3">
          {!revoking && (
            <Button variant="outline" onClick={() => setRevokingId(existing.id)}>
              <Unlink aria-hidden="true" />
              {t('revoke.button')}
            </Button>
          )}
          {revoking && (
            <RevokeFeed
              id={revokeId}
              feedId={existing.id}
              onCancel={() => setRevokingId(null)}
              onRevoked={() => {
                setRevokingId(null);
                setCreated(null);
              }}
            />
          )}
        </div>
      )}
    </div>
  );
}

/** The account's link of the subject: when it was made and last used; its address is gone. */
function ExistingLink({ feed }: { feed: CalendarFeed }) {
  const { t } = useTranslation('feeds');
  const day = useSchoolDay();
  const created = day(feed.createdAt);
  // "Never" only when no calendar fetched it; a date only once the school's zone is known.
  const lastUsed = feed.lastUsedAt === undefined ? null : day(feed.lastUsedAt);
  return (
    <div className="space-y-1 rounded-lg border p-3">
      {created && <p className="font-medium">{t('existing', { created })}</p>}
      {lastUsed !== undefined && (
        <p className="text-muted-foreground">
          {lastUsed === null ? t('neverUsed') : t('lastUsed', { date: lastUsed })}
        </p>
      )}
      <p className="text-muted-foreground">{t('lostLink')}</p>
    </div>
  );
}

/** The new link, shown this once: copy it, open it in the calendar app, download it. */
function CreatedLink({ created }: { created: NewCalendarFeed }) {
  const { t } = useTranslation('feeds');
  const url = subscriptionUrl(created);
  const inputId = useId();
  const titleId = useId();
  const title = useRef<HTMLHeadingElement>(null);
  const [downloading, setDownloading] = useState(false);

  // The button that made the link is gone: the focus moves to what it made.
  useEffect(() => title.current?.focus(), []);

  const copy = async () => {
    if (!url) return;
    try {
      await navigator.clipboard.writeText(url);
      toast.success(t('created.copied'));
    } catch {
      toast.error(t('created.copyFailed'));
    }
  };

  const download = async () => {
    setDownloading(true);
    try {
      await downloadCalendarFeed(created);
    } catch {
      toast.error(t('created.downloadFailed'));
    } finally {
      setDownloading(false);
    }
  };

  return (
    <section
      aria-labelledby={titleId}
      className="space-y-3 rounded-lg border border-primary/30 bg-primary/5 p-3"
    >
      <h3 id={titleId} ref={title} tabIndex={-1} className="font-medium outline-none">
        {t('created.title')}
      </h3>
      {url ? (
        <>
          <p>{t('created.once')}</p>
          <div className="grid gap-1.5">
            <Label htmlFor={inputId}>{t('created.url')}</Label>
            <Input
              id={inputId}
              readOnly
              value={url}
              className="font-mono text-xs"
              onFocus={(event) => event.currentTarget.select()}
            />
          </div>
        </>
      ) : (
        <Alert>
          <CircleAlert aria-hidden="true" />
          <AlertDescription>{t('created.demo')}</AlertDescription>
        </Alert>
      )}
      <div className="flex flex-wrap gap-2">
        {url && (
          <>
            <Button onClick={() => void copy()}>
              <Copy aria-hidden="true" />
              {t('created.copy')}
            </Button>
            <Button asChild variant="outline">
              <a href={webcalUrl(url)}>
                <CalendarPlus aria-hidden="true" />
                {t('created.open')}
              </a>
            </Button>
          </>
        )}
        <Button
          variant={url ? 'outline' : 'default'}
          disabled={downloading}
          onClick={() => void download()}
        >
          <Download aria-hidden="true" />
          {t('created.download')}
        </Button>
      </div>
    </section>
  );
}
