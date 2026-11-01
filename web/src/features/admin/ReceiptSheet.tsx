import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Ban, Download, XIcon } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { conflictReason } from '@/api/conflict';
import { openDownload } from '@/api/download';
import {
  ApiError,
  cancelReceiptMutation,
  type Receipt,
  receiptPdfPath,
  receiptQuery,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { formatMoney, formatRate } from '@/lib/money';
import { shownName } from '@/lib/personName';
import { formatDate } from '@/lib/schoolTime';
import { TextareaField } from './crud/fields';
import { LoadError } from './crud/LoadError';
import { ReceiptSettingsNotice } from './ReceiptSettingsNotice';

/** A receipt's kind as a badge: a sale, a canceled sale, or a cancellation. */
export function ReceiptKindBadge({ receipt }: { receipt: Receipt }) {
  const { t } = useTranslation('receipts');
  if (receipt.kind === 'cancellation') {
    return <Badge variant="outline">{t('kind.cancellation')}</Badge>;
  }
  return receipt.canceledBy ? (
    <Badge variant="destructive">{t('kind.canceled')}</Badge>
  ) : (
    <Badge variant="secondary">{t('kind.sale')}</Badge>
  );
}

/**
 * A receipt in a side sheet (closed without `receiptId`): the issuer as
 * printed, the recipient, the lines with net, VAT and gross, the payment
 * method, the tax note, the link between a sale and its cancellation
 * (`onOpen` shows the other), its PDF, and the cancellation of a sale whose
 * pass paid for nothing yet.
 */
export function ReceiptSheet({
  receiptId,
  onOpen,
  onClose,
}: {
  receiptId: string | undefined;
  onOpen: (receiptId: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation('receipts');
  // Keep the last receipt while the sheet animates out.
  const [shown, setShown] = useState(receiptId);
  if (receiptId !== undefined && receiptId !== shown) setShown(receiptId);
  return (
    <Sheet open={receiptId !== undefined} onOpenChange={(next) => !next && onClose()}>
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-full overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-lg"
      >
        {shown && <ReceiptDetail key={shown} receiptId={shown} onOpen={onOpen} />}
        <SheetClose asChild>
          <Button variant="ghost" size="icon-sm" className="absolute top-3 right-3">
            <XIcon aria-hidden="true" />
            <span className="sr-only">{t('detail.close')}</span>
          </Button>
        </SheetClose>
      </SheetContent>
    </Sheet>
  );
}

function ReceiptDetail({
  receiptId,
  onOpen,
}: {
  receiptId: string;
  onOpen: (receiptId: string) => void;
}) {
  const { t } = useTranslation('receipts');
  const receipt = useQuery(receiptQuery(receiptId));
  const [canceling, setCanceling] = useState({ open: false, seq: 0 });

  if (!receipt.data) {
    return (
      <>
        <SheetHeader className="pr-12">
          <SheetTitle className="text-lg">{t('detail.title')}</SheetTitle>
          <SheetDescription className="sr-only">{t('detail.title')}</SheetDescription>
        </SheetHeader>
        <div className="px-4 pb-4">
          {receipt.error instanceof ApiError && receipt.error.status === 404 ? (
            <p className="text-muted-foreground">{t('detail.notFound')}</p>
          ) : receipt.isError ? (
            <LoadError message={t('detail.loadError')} onRetry={() => void receipt.refetch()} />
          ) : (
            <div role="status" className="space-y-3">
              <span className="sr-only">{t('detail.loading')}</span>
              <Skeleton className="h-40 w-full" aria-hidden="true" />
            </div>
          )}
        </div>
      </>
    );
  }

  const r = receipt.data;
  return (
    <>
      <SheetHeader className="pr-12">
        <SheetTitle className="text-lg">
          {t(r.kind === 'cancellation' ? 'detail.cancellationTitle' : 'detail.saleTitle', {
            number: r.number,
          })}
        </SheetTitle>
        <SheetDescription>{t('detail.description')}</SheetDescription>
      </SheetHeader>
      <div className="space-y-5 px-4 pb-4 text-sm">
        <div className="flex flex-wrap items-center gap-2">
          <ReceiptKindBadge receipt={r} />
          <DownloadButton receipt={r} />
          {r.cancelable && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setCanceling((c) => ({ open: true, seq: c.seq + 1 }))}
            >
              <Ban aria-hidden="true" />
              {t('cancel.open')}
            </Button>
          )}
        </div>
        <ReceiptFacts receipt={r} onOpen={onOpen} />
        <Lines receipt={r} />
      </div>
      <CancelReceiptDialog
        key={canceling.seq}
        receipt={r}
        open={canceling.open}
        onClose={() => setCanceling((c) => ({ ...c, open: false }))}
        onCanceled={(cancellation) => onOpen(cancellation.id)}
      />
    </>
  );
}

/** Downloads the receipt's PDF, as it was issued. */
export function DownloadButton({
  receipt,
  mine = false,
  label,
}: {
  receipt: Pick<Receipt, 'id' | 'number'>;
  mine?: boolean;
  label?: string;
}) {
  const { t } = useTranslation('receipts');
  const [pending, setPending] = useState(false);
  const download = async () => {
    setPending(true);
    try {
      await openDownload(receiptPdfPath(receipt.id, { mine }));
    } catch {
      toast.error(t('download.failed'));
    } finally {
      setPending(false);
    }
  };
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={pending}
      aria-label={label ?? t('download.label', { number: receipt.number })}
      onClick={() => void download()}
    >
      <Download aria-hidden="true" />
      {pending ? t('download.pending') : t('download.button')}
    </Button>
  );
}

function ReceiptFacts({ receipt: r, onOpen }: { receipt: Receipt; onOpen: (id: string) => void }) {
  const { t, i18n } = useTranslation('receipts');
  const lang = i18n.resolvedLanguage ?? 'en';
  const day = (date: string) =>
    formatDate(date, lang, { day: 'numeric', month: 'long', year: 'numeric' });
  const link = (ref: NonNullable<Receipt['cancels']>) => (
    <button
      type="button"
      className="font-medium underline underline-offset-4"
      onClick={() => onOpen(ref.id)}
    >
      {t('detail.refLabel', { number: ref.number, date: day(ref.issueDate) })}
    </button>
  );
  const issuer = [
    r.issuer.name,
    r.issuer.address,
    t('detail.taxNumber', { number: r.issuer.taxNumber }),
    r.issuer.email,
    r.issuer.phone,
  ]
    .filter(Boolean)
    .join('\n');
  const rows: [string, ReactNode][] = [
    [t('detail.issued'), day(r.issueDate)],
    [t('detail.issuer'), issuer],
    [
      t('detail.recipient'),
      [r.recipientName, r.recipientAddress, r.recipientEmail].filter(Boolean).join('\n'),
    ],
    ...(r.payer && shownName(r.payer) !== r.recipientName
      ? [[t('detail.payer'), shownName(r.payer)] as [string, ReactNode]]
      : []),
    [t('method.label'), t(`method.${r.paymentMethod}`)],
    [t('detail.taxMode'), t(`settings.taxModes.${r.taxMode}`)],
    ...(r.taxNote ? [[t('detail.taxNote'), r.taxNote] as [string, ReactNode]] : []),
    ...(r.reason ? [[t('detail.reason'), r.reason] as [string, ReactNode]] : []),
    ...(r.cancels ? [[t('detail.cancels'), link(r.cancels)] as [string, ReactNode]] : []),
    ...(r.canceledBy ? [[t('detail.canceledBy'), link(r.canceledBy)] as [string, ReactNode]] : []),
  ];
  return (
    <dl className="grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-[auto_1fr] sm:gap-y-2">
      {rows.map(([term, value]) => (
        <div key={term} className="contents">
          <dt className="text-muted-foreground">{term}</dt>
          <dd className="mb-2 min-w-0 break-words whitespace-pre-line sm:mb-0">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** The lines with their net, VAT and gross, and the totals. */
function Lines({ receipt: r }: { receipt: Receipt }) {
  const { t, i18n } = useTranslation('receipts');
  const lang = i18n.resolvedLanguage ?? 'en';
  const money = (minor: number) => formatMoney(minor, r.currency, lang);
  return (
    <div className="space-y-2">
      <h3 className="font-medium">{t('detail.lines')}</h3>
      <ul aria-label={t('detail.lines')} className="divide-y rounded-lg border">
        {r.lines.map((line) => (
          <li key={line.position} className="space-y-1 px-3 py-2">
            <p className="font-medium break-words">
              {line.quantity > 1 ? `${line.quantity} × ${line.description}` : line.description}
            </p>
            <p className="text-muted-foreground tabular-nums">
              {t('detail.lineAmounts', {
                net: money(line.net),
                rate: formatRate(line.vatRate, lang),
                vat: money(line.vat),
                gross: money(line.gross),
              })}
            </p>
          </li>
        ))}
      </ul>
      <dl className="ml-auto grid w-fit grid-cols-[auto_auto] gap-x-6 gap-y-0.5 tabular-nums">
        <dt className="text-muted-foreground">{t('detail.net')}</dt>
        <dd className="text-right">{money(r.net)}</dd>
        <dt className="text-muted-foreground">{t('detail.vat')}</dt>
        <dd className="text-right">{money(r.vat)}</dd>
        <dt className="font-medium">{t('detail.gross')}</dt>
        <dd className="text-right font-medium">{money(r.gross)}</dd>
      </dl>
    </div>
  );
}

/**
 * Asks before canceling a sale receipt (Storno), with an optional reason
 * printed on the cancellation; the cancellation voids the pass. Says why
 * the API refuses: the pass paid for visits, the receipt is canceled
 * already, or the receipt settings are incomplete.
 */
function CancelReceiptDialog({
  receipt,
  open,
  onClose,
  onCanceled,
}: {
  receipt: Receipt;
  open: boolean;
  onClose: () => void;
  onCanceled: (cancellation: Receipt) => void;
}) {
  const { t } = useTranslation('receipts');
  const queryClient = useQueryClient();
  const cancel = useMutation(cancelReceiptMutation(queryClient));
  const [reason, setReason] = useState('');
  const reasonCode = conflictReason(cancel.error);
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !cancel.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('cancel.title', { number: receipt.number })}</AlertDialogTitle>
          <AlertDialogDescription>{t('cancel.description')}</AlertDialogDescription>
        </AlertDialogHeader>
        {reasonCode === 'receipt-settings-incomplete' ? (
          <ReceiptSettingsNotice />
        ) : (
          <ProblemAlert
            error={cancel.error}
            conflicts={{
              'pass-used': t('cancel.passUsed'),
              'already-canceled': t('cancel.alreadyCanceled'),
              cancellation: t('cancel.isCancellation'),
              'event-attended': t('cancel.eventAttended'),
              'registration-open': t('cancel.registrationOpen'),
            }}
            messages={{ 404: t('detail.notFound') }}
          />
        )}
        <TextareaField
          label={t('cancel.reason')}
          rows={2}
          maxLength={200}
          value={reason}
          onChange={(e) => setReason(e.target.value)}
        />
        <AlertDialogFooter>
          <AlertDialogCancel disabled={cancel.isPending}>{t('cancel.keep')}</AlertDialogCancel>
          <Button
            variant="destructive"
            disabled={cancel.isPending || reasonCode !== undefined}
            onClick={() =>
              cancel.mutate(
                { receiptId: receipt.id, reason: reason.trim() || undefined },
                {
                  onSuccess: (cancellation) => {
                    toast.success(t('cancel.done', { number: cancellation.number }));
                    onClose();
                    onCanceled(cancellation);
                  },
                },
              )
            }
          >
            <Ban aria-hidden="true" />
            {cancel.isPending ? t('cancel.canceling') : t('cancel.confirm')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
