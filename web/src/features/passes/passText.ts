import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import type { BookingPayment, Pass, PassType } from '@/api/queries';
import { formatDate, type SchoolDate, yearUnlessIn } from '@/lib/schoolTime';

/** A school date of a pass, e.g. 'Oct 26, 2026'. */
function useDay() {
  const { i18n } = useTranslation();
  const lang = i18n.resolvedLanguage ?? 'en';
  return (date: SchoolDate, today?: SchoolDate) =>
    formatDate(date, lang, {
      day: 'numeric',
      month: 'short',
      ...(today === undefined ? { year: 'numeric' } : yearUnlessIn(date, today)),
    });
}

/** What a pass type gives: '10 visits, valid 180 days', 'Unlimited visits for 30 days'. */
export function passTypeTerms(
  t: TFunction<'passes'>,
  type: Pick<PassType, 'kind' | 'visits' | 'validityDays'>,
): string {
  if (type.kind === 'time') return t('terms.time', { count: type.validityDays });
  return type.validityDays === 0
    ? t('terms.visitsNoExpiry', { count: type.visits ?? 0 })
    : t('terms.visits', { count: type.visits ?? 0, days: type.validityDays });
}

/** The words of passes and pass types. */
export function usePassText() {
  const { t } = useTranslation('passes');
  const day = useDay();
  return {
    typeTerms: (type: Pick<PassType, 'kind' | 'visits' | 'validityDays'>) => passTypeTerms(t, type),
    /** The visits of a pass: '7 of 10 visits left' or 'Unlimited visits'. */
    visits: (pass: Pick<Pass, 'kind' | 'visitsRemaining' | 'visitsTotal'>) =>
      pass.kind === 'time'
        ? t('visits.unlimited')
        : t('visits.left', { count: pass.visitsRemaining, total: pass.visitsTotal }),
    /** The days of a pass: 'Oct 1 – Dec 31, 2026' or 'from Oct 1, 2026, no expiry'. */
    days: (pass: Pick<Pass, 'validFrom' | 'validUntil'>, today?: SchoolDate) =>
      pass.validUntil === undefined
        ? t('days.noExpiry', { from: day(pass.validFrom, today) })
        : t('days.range', { from: day(pass.validFrom, today), until: day(pass.validUntil, today) }),
    day,
  };
}

/** The badge variant of each way a booking is paid; unpaid stands out. */
export const paymentVariant = {
  paid: 'secondary',
  unpaid: 'destructive',
  free: 'outline',
  pending: 'outline',
} as const satisfies Record<BookingPayment['status'], string>;

/**
 * How a booking is paid, in words: 'Paid', 'Unpaid', 'Free', 'Pending';
 * `detailed` names the pass that paid and the visits it has left.
 */
export function paymentText(
  t: TFunction<'passes'>,
  payment: BookingPayment,
  detailed = false,
): string {
  const pass = payment.pass;
  if (payment.status !== 'paid' || !detailed || pass === undefined) {
    return t(`payment.${payment.status}`);
  }
  return pass.kind === 'time'
    ? t('payment.paidTime', { pass: pass.name })
    : t('payment.paidVisits', { pass: pass.name, count: pass.visitsRemaining });
}
