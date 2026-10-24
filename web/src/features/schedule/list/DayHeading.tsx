import { useTranslation } from 'react-i18next';
import { formatDayHeading, type SchoolDate } from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import { type DayTone, dayToneClass } from './days';

/**
 * The sticky heading of a school day in a list of lessons, colored by the
 * day's distance from today; today and tomorrow say so, and today is the
 * current date for assistive technology.
 */
export function DayHeading({ id, date, tone }: { id: string; date: SchoolDate; tone: DayTone }) {
  const { t, i18n } = useTranslation('schedule');
  const heading = formatDayHeading(date, i18n.resolvedLanguage ?? 'en');
  return (
    <h2
      id={id}
      aria-current={tone === 'today' ? 'date' : undefined}
      className={cn(
        'sticky top-0 z-10 rounded-md px-4 py-2 font-heading text-xl font-medium tracking-tight shadow-[0_6px_16px_-12px_oklch(0.3_0.08_350/0.5)]',
        dayToneClass[tone],
      )}
    >
      {tone === 'today' || tone === 'tomorrow' ? t(`list.${tone}`, { day: heading }) : heading}
    </h2>
  );
}
