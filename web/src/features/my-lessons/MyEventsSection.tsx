import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { myEventsQuery, publicSettingsQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { LoadError } from '@/features/admin/crud/LoadError';
import { DownloadButton } from '@/features/admin/ReceiptSheet';
import { RegistrationControls } from '@/features/events/RegistrationControls';
import { useEventView } from '@/features/events/useEventView';
import { useMoney } from '@/features/passes/useMoney';
import { shownName } from '@/lib/personName';
import { Loading } from './Loading';

export function MyEventsSection() {
  const { t } = useTranslation('events');
  const titleId = useId();
  const events = useQuery(myEventsQuery);
  const settings = useQuery(publicSettingsQuery);
  const timeZone = settings.data?.timezone ?? 'UTC';
  const view = useEventView(timeZone);
  const money = useMoney();
  if (events.data?.length === 0) return null;

  return (
    <section aria-labelledby={titleId} className="space-y-4">
      <h2 id={titleId} className="font-heading text-2xl font-medium tracking-tight">
        {t('mine')}
      </h2>
      {events.isError || settings.isError ? (
        <LoadError
          message={t('loadRegistrationError')}
          onRetry={() => {
            if (events.isError) void events.refetch();
            if (settings.isError) void settings.refetch();
          }}
        />
      ) : events.data && settings.data ? (
        <ul className="divide-y rounded-xl border">
          {events.data.map((item) => {
            const { registration: r, event } = item;
            const session = view(event).nextSession;
            return (
              <li className="space-y-2 p-4" key={r.id}>
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  <Link
                    to="/events/$slug"
                    params={{ slug: event.slug }}
                    className="font-semibold underline-offset-4 hover:underline"
                  >
                    {event.title}
                  </Link>
                  <Badge variant="secondary">{t(`states.${r.status}`)}</Badge>
                </div>
                {session && (
                  <p className="text-sm text-muted-foreground">
                    {session.date} · {session.time}
                  </p>
                )}
                <p className="text-sm">
                  {shownName(r.person)} ·{' '}
                  {r.price === 0
                    ? t('free')
                    : r.payment === 'unpaid' && r.status !== 'registered'
                      ? money.format(r.price)
                      : t(`payment.${r.payment}`, { amount: money.format(r.price) })}
                </p>
                <RegistrationControls item={item} timeZone={timeZone} />
                {(r.receiptId || r.cancellationReceiptId) && (
                  <div className="flex flex-wrap gap-2">
                    {r.receiptId && (
                      <DownloadButton
                        mine
                        receipt={{ id: r.receiptId }}
                        visibleLabel={t('receipt')}
                      />
                    )}
                    {r.cancellationReceiptId && (
                      <DownloadButton
                        mine
                        receipt={{ id: r.cancellationReceiptId }}
                        visibleLabel={t('refundReceipt')}
                      />
                    )}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      ) : (
        <Loading />
      )}
    </section>
  );
}
