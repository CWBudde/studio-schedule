import { useQuery } from '@tanstack/react-query';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { myReceiptsQuery, type Receipt } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { LoadError } from '@/features/admin/crud/LoadError';
import { DownloadButton } from '@/features/admin/ReceiptSheet';
import { formatMoney } from '@/lib/money';
import { formatDate } from '@/lib/schoolTime';
import { Loading } from './Loading';

/**
 * "My receipts": the receipts of the passes the signed-in person paid for
 * (also those of their children), newest first, each with its PDF. Left
 * out for someone who paid for nothing.
 */
export function MyReceiptsSection() {
  const { t } = useTranslation('receipts');
  const headingId = useId();
  const receipts = useQuery(myReceiptsQuery);

  if (receipts.data && receipts.data.length === 0) return null;
  return (
    <section aria-labelledby={headingId} className="space-y-4">
      <h2 id={headingId} className="font-heading text-2xl font-medium tracking-tight">
        {t('mine.title')}
      </h2>
      {receipts.data ? (
        <ul aria-label={t('mine.title')} className="divide-y rounded-xl ring-1 ring-foreground/10">
          {receipts.data.map((r) => (
            <ReceiptRow key={r.id} receipt={r} />
          ))}
        </ul>
      ) : receipts.isError ? (
        <LoadError message={t('mine.loadError')} onRetry={() => void receipts.refetch()} />
      ) : (
        <Loading />
      )}
    </section>
  );
}

function ReceiptRow({ receipt: r }: { receipt: Receipt }) {
  const { t, i18n } = useTranslation('receipts');
  const lang = i18n.resolvedLanguage ?? 'en';
  const what = r.lines.map((l) => l.description).join(', ');
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 text-sm">
      <div className="min-w-0 flex-1 space-y-0.5">
        <p className="flex flex-wrap items-center gap-2">
          <span className="font-medium tabular-nums">{r.number}</span>
          {r.kind === 'cancellation' && <Badge variant="outline">{t('kind.cancellation')}</Badge>}
          {r.canceledBy && <Badge variant="destructive">{t('kind.canceled')}</Badge>}
        </p>
        <p className="break-words text-muted-foreground">
          {t('mine.line', {
            date: formatDate(r.issueDate, lang, {
              day: 'numeric',
              month: 'short',
              year: 'numeric',
            }),
            what,
            amount: formatMoney(r.gross, r.currency, lang),
          })}
        </p>
      </div>
      <DownloadButton receipt={r} mine />
    </li>
  );
}
