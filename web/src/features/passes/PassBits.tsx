import { useTranslation } from 'react-i18next';
import type { BookingPayment } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import type { PassState } from './passes';
import { paymentText, paymentVariant } from './passText';

const stateVariant = {
  usable: 'default',
  voided: 'destructive',
  notYetValid: 'outline',
  usedUp: 'secondary',
  expired: 'secondary',
} as const;

/** The state of a pass as a badge. */
export function PassStateBadge({ state }: { state: PassState }) {
  const { t } = useTranslation('passes');
  return <Badge variant={stateVariant[state]}>{t(`states.${state}`)}</Badge>;
}

/**
 * How a booking is paid, as a badge: by which pass (and the visits it has
 * left), unpaid (to settle at the desk), free, or pending attendance.
 */
export function PaymentBadge({ payment }: { payment: BookingPayment | undefined }) {
  const { t } = useTranslation('passes');
  if (!payment) return null;
  return <Badge variant={paymentVariant[payment.status]}>{paymentText(t, payment, true)}</Badge>;
}
