import { useQuery } from '@tanstack/react-query';
import { UserPlus } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type AdminEvent, type EventRegistration, eventRegistrationsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useMoney } from '@/features/passes/useMoney';
import { shownName } from '@/lib/personName';
import { DownloadButton } from '../ReceiptSheet';
import { LoadError } from '../crud/LoadError';
import { AddRegistrationDialog } from './AddRegistrationDialog';
import { CancelRegistrationDialog } from './CancelRegistrationDialog';
import { MarkPaidDialog } from './MarkPaidDialog';

type Action = { kind: 'pay' | 'cancel'; registration: EventRegistration; name: string };

export function EventRegistrations({ event }: { event: AdminEvent }) {
  const { t } = useTranslation('adminEvents');
  const registrations = useQuery(eventRegistrationsQuery(event.id));
  const [adding, setAdding] = useState(false);
  const [action, setAction] = useState<Action>();
  const { format: money } = useMoney();

  return (
    <Card>
      <CardHeader className="flex flex-wrap items-center justify-between gap-2">
        <CardTitle>
          <h2>{t('registrations.title')}</h2>
        </CardTitle>
        {event.status === 'published' && (
          <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
            <UserPlus aria-hidden="true" />
            {t('registrations.add')}
          </Button>
        )}
      </CardHeader>
      <CardContent>
        {registrations.isError ? (
          <LoadError
            message={t('registrations.loadError')}
            onRetry={() => void registrations.refetch()}
          />
        ) : !registrations.data ? (
          <Skeleton className="h-20" />
        ) : registrations.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('registrations.empty')}</p>
        ) : (
          <Table aria-label={t('registrations.title')}>
            <TableHeader>
              <TableRow>
                <TableHead>{t('registrations.participant')}</TableHead>
                <TableHead>{t('status')}</TableHead>
                <TableHead>{t('registrations.payment')}</TableHead>
                <TableHead>{t('registrations.price')}</TableHead>
                <TableHead>{t('registrations.receipts')}</TableHead>
                <TableHead>
                  <span className="sr-only">{t('registrations.actions')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {registrations.data.map((registration) => {
                const name = registration.person
                  ? shownName(registration.person)
                  : (registration.guest?.name ?? t('registrations.guest'));
                const canPay =
                  registration.status === 'registered' && registration.payment === 'unpaid';
                const canCancel = registration.status !== 'canceled';
                return (
                  <TableRow key={registration.id}>
                    <TableCell className="font-medium">{name}</TableCell>
                    <TableCell>{t(`registrations.status.${registration.status}`)}</TableCell>
                    <TableCell>
                      {t(`registrations.paymentStatus.${registration.payment}`)}
                    </TableCell>
                    <TableCell className="tabular-nums">
                      {registration.price ? money(registration.price) : t('free')}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-2">
                        {registration.receiptId && (
                          <DownloadButton
                            receipt={{ id: registration.receiptId }}
                            visibleLabel={t('registrations.downloadReceipt')}
                          />
                        )}
                        {registration.cancellationReceiptId && (
                          <DownloadButton
                            receipt={{ id: registration.cancellationReceiptId }}
                            visibleLabel={t('registrations.downloadCancellation')}
                          />
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-2">
                        {canPay && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => setAction({ kind: 'pay', registration, name })}
                          >
                            {t('registrations.markPaid')}
                          </Button>
                        )}
                        {canCancel && (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => setAction({ kind: 'cancel', registration, name })}
                          >
                            {t('registrations.cancel')}
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
      {adding && <AddRegistrationDialog event={event} onClose={() => setAdding(false)} />}
      {action?.kind === 'pay' && (
        <MarkPaidDialog {...action} onClose={() => setAction(undefined)} />
      )}
      {action?.kind === 'cancel' && (
        <CancelRegistrationDialog {...action} onClose={() => setAction(undefined)} />
      )}
    </Card>
  );
}
