import { useQuery } from '@tanstack/react-query';
import { CalendarDays, Unlink } from 'lucide-react';
import { useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type CalendarFeed, calendarFeedsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { LoadError } from '@/features/admin/crud/LoadError';
import { RevokeFeed } from './RevokeFeed';
import { useFeedLabel, useSchoolDay } from './subject';

/**
 * The account's active calendar feeds in the account settings: what each
 * shows, when it was created and last fetched, and a way to revoke it. Their
 * links cannot be shown again; the "Subscribe" dialogs make new ones.
 */
export function CalendarFeedsCard() {
  const { t } = useTranslation('feeds');
  const titleId = useId();
  const feeds = useQuery(calendarFeedsQuery);

  return (
    <Card role="region" aria-labelledby={titleId}>
      <CardHeader>
        <CardTitle>
          <h2 id={titleId} className="font-heading text-lg font-semibold">
            {t('card.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('card.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {feeds.isPending ? (
          <div role="status">
            <span className="sr-only">{t('loading')}</span>
            <Skeleton className="h-16 w-full" aria-hidden="true" />
          </div>
        ) : feeds.isError ? (
          <LoadError message={t('loadFailed')} onRetry={() => void feeds.refetch()} />
        ) : feeds.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('card.empty')}</p>
        ) : (
          <ul className="space-y-3">
            {feeds.data.map((feed) => (
              <FeedItem key={feed.id} feed={feed} />
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

function FeedItem({ feed }: { feed: CalendarFeed }) {
  const { t } = useTranslation('feeds');
  const label = useFeedLabel()(feed);
  const day = useSchoolDay();
  const created = day(feed.createdAt);
  const lastUsed = feed.lastUsedAt && day(feed.lastUsedAt);
  const [confirming, setConfirming] = useState(false);
  const revokeButton = useRef<HTMLButtonElement>(null);
  const confirmationId = useId();

  return (
    <li className="rounded-lg border p-3">
      <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 items-start gap-2">
          <CalendarDays
            className="mt-0.5 size-4 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <div className="min-w-0 space-y-0.5">
            <p className="font-medium break-words">{label}</p>
            {created && (
              <p className="text-sm text-muted-foreground">
                {t('card.created', { date: created })} ·{' '}
                {lastUsed ? t('card.lastUsed', { date: lastUsed }) : t('card.neverUsed')}
              </p>
            )}
          </div>
        </div>
        <Button
          ref={revokeButton}
          variant="outline"
          size="sm"
          aria-label={t('revoke.named', { label })}
          aria-expanded={confirming}
          aria-controls={confirming ? confirmationId : undefined}
          onClick={() => setConfirming((c) => !c)}
        >
          <Unlink aria-hidden="true" />
          {t('revoke.button')}
        </Button>
      </div>
      {confirming && (
        <div className="mt-3">
          <RevokeFeed
            id={confirmationId}
            feedId={feed.id}
            onCancel={() => {
              setConfirming(false);
              revokeButton.current?.focus();
            }}
          />
        </div>
      )}
    </li>
  );
}
