import { useTranslation } from 'react-i18next';
import type { BookingPayment } from '@/api/queries';
import { paymentText, paymentVariant } from '@/features/passes/passText';
import type { RowBadge } from './MyLessonRow';

/**
 * How a booking is paid as a row badge: by a pass, or unpaid (to settle at
 * the desk); nothing for a free booking or one whose attendance decides.
 */
export function usePaymentBadge(): (payment: BookingPayment | undefined) => RowBadge[] {
  const { t } = useTranslation('passes');
  return (payment) =>
    payment?.status === 'paid' || payment?.status === 'unpaid'
      ? [{ label: paymentText(t, payment), variant: paymentVariant[payment.status] }]
      : [];
}
