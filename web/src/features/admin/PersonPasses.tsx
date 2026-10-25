import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { Ban, Gift, ShoppingBag } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { conflictReason } from '@/api/conflict';
import {
  type AdminPass,
  type PersonDetail,
  personPassesQuery,
  publicSettingsQuery,
  voidPassMutation,
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
import { Button } from '@/components/ui/button';
import { PassStateBadge } from '@/features/passes/PassBits';
import { usePassText } from '@/features/passes/passText';
import { passState } from '@/features/passes/passes';
import { useMoney } from '@/features/passes/useMoney';
import { useServerClock } from '@/features/schedule/useServerClock';
import { type SchoolDate, schoolDateOf, todayIn } from '@/lib/schoolTime';
import { LoadError } from './crud/LoadError';
import { personName } from './crud/person';
import { PassSaleDialog, type PassSaleMode } from './PassSaleDialog';
import { Section } from './PersonSection';

/**
 * The passes of a person for the office: each with its type, visits, days,
 * state, who paid and how it came (sold or given), and a void while it is
 * active; a pass can be sold or given away here.
 */
export function PersonPasses({ person }: { person: PersonDetail }) {
  const { t } = useTranslation('passes');
  const passes = useQuery(personPassesQuery(person.id));
  const settings = useQuery(publicSettingsQuery);
  const now = useServerClock();
  const name = personName(person);
  const [sale, setSale] = useState<{ mode: PassSaleMode; open: boolean; seq: number }>({
    mode: 'sell',
    open: false,
    seq: 0,
  });
  const [voiding, setVoiding] = useState<{ pass: AdminPass | null; open: boolean; seq: number }>({
    pass: null,
    open: false,
    seq: 0,
  });
  const openSale = (mode: PassSaleMode) => setSale((s) => ({ mode, open: true, seq: s.seq + 1 }));
  const today = settings.data ? todayIn(settings.data.timezone, now) : undefined;

  return (
    <Section
      title={t('person.title')}
      description={t('person.description', { name })}
      action={
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => openSale('sell')}>
            <ShoppingBag aria-hidden="true" />
            {t('person.sell')}
          </Button>
          <Button variant="outline" size="sm" onClick={() => openSale('assign')}>
            <Gift aria-hidden="true" />
            {t('person.assign')}
          </Button>
        </div>
      }
    >
      {passes.data && today && settings.data ? (
        passes.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('person.empty', { name })}</p>
        ) : (
          <ul aria-label={t('person.list', { name })} className="divide-y rounded-lg border">
            {passes.data.map((pass) => (
              <PassItem
                key={pass.id}
                pass={pass}
                ownerId={person.id}
                today={today}
                timeZone={settings.data.timezone}
                onVoid={() => setVoiding((v) => ({ pass, open: true, seq: v.seq + 1 }))}
              />
            ))}
          </ul>
        )
      ) : passes.isError || settings.isError ? (
        <LoadError
          message={t('person.loadError')}
          onRetry={() => {
            void passes.refetch();
            void settings.refetch();
          }}
        />
      ) : (
        <p role="status" className="text-sm text-muted-foreground">
          {t('person.loading')}
        </p>
      )}
      <PassSaleDialog
        key={`sale-${sale.seq}`}
        mode={sale.mode}
        open={sale.open}
        person={person}
        onClose={() => setSale((s) => ({ ...s, open: false }))}
      />
      <VoidPassDialog
        key={`void-${voiding.seq}`}
        pass={voiding.pass}
        open={voiding.open}
        onClose={() => setVoiding((v) => ({ ...v, open: false }))}
      />
    </Section>
  );
}

function PassItem({
  pass,
  ownerId,
  today,
  timeZone,
  onVoid,
}: {
  pass: AdminPass;
  ownerId: string;
  today: SchoolDate;
  timeZone: string;
  onVoid: () => void;
}) {
  const { t } = useTranslation('passes');
  const text = usePassText();
  const money = useMoney();
  const sold = text.day(schoolDateOf(pass.soldAt, timeZone));
  const payer = pass.payer && pass.payer.id !== ownerId ? pass.payer : undefined;
  return (
    <li className="space-y-1 px-3 py-2.5 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium break-words">{pass.passType.name}</span>
        <PassStateBadge state={passState(pass, today)} />
        {pass.status === 'active' && (
          <Button
            variant="outline"
            size="xs"
            className="ml-auto"
            aria-label={t('person.voidLabel', { pass: pass.passType.name, date: sold })}
            onClick={onVoid}
          >
            <Ban aria-hidden="true" />
            {t('person.void')}
          </Button>
        )}
      </div>
      <p>
        {text.visits(pass)} · {text.days(pass)}
      </p>
      <p className="text-muted-foreground">
        {pass.origin === 'sale'
          ? t('person.sold', { date: sold, price: money.format(pass.pricePaid) })
          : t('person.given', { date: sold })}
        {payer && ` · ${t('person.paidBy', { name: payer.name })}`}
      </p>
      <p className="flex flex-wrap items-center gap-x-3 text-muted-foreground">
        {t('person.uses', { count: pass.useCount })}
        {pass.receiptId && (
          <Link
            to="/admin/receipts"
            search={{ receipt: pass.receiptId }}
            aria-label={t('person.receiptLabel', { pass: pass.passType.name, date: sold })}
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            {t('person.receipt')}
          </Link>
        )}
      </p>
      {pass.note && (
        <p className="break-words whitespace-pre-line text-muted-foreground">
          {t('person.note', { note: pass.note })}
        </p>
      )}
    </li>
  );
}

/**
 * Asks before voiding a pass: it pays for nothing more, what it paid for
 * stays paid. A pass voided meanwhile (409 `already-voided`) says so.
 */
function VoidPassDialog({
  pass,
  open,
  onClose,
}: {
  /** The pass, kept while the dialog closes; a fresh dialog per opening (key). */
  pass: AdminPass | null;
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation('passes');
  const queryClient = useQueryClient();
  const voidPass = useMutation(voidPassMutation(queryClient));
  const name = pass?.passType.name ?? '';
  return (
    <AlertDialog
      open={open && pass !== null}
      onOpenChange={(next) => {
        if (!next && !voidPass.isPending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t('void.title', { pass: name })}</AlertDialogTitle>
          <AlertDialogDescription>{t('void.description')}</AlertDialogDescription>
        </AlertDialogHeader>
        <ProblemAlert
          error={voidPass.error}
          conflicts={{ 'already-voided': t('void.alreadyVoided', { pass: name }) }}
          messages={{ 404: t('void.notFound') }}
        />
        <AlertDialogFooter>
          <AlertDialogCancel disabled={voidPass.isPending}>{t('void.keep')}</AlertDialogCancel>
          {conflictReason(voidPass.error) !== 'already-voided' && (
            <Button
              variant="destructive"
              disabled={voidPass.isPending}
              onClick={() =>
                pass &&
                voidPass.mutate(pass.id, {
                  onSuccess: () => {
                    toast.success(t('void.done', { pass: name }));
                    onClose();
                  },
                })
              }
            >
              <Ban aria-hidden="true" />
              {voidPass.isPending ? t('void.voiding') : t('void.confirm')}
            </Button>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
