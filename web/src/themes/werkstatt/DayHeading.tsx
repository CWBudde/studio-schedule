import { ChevronLeft, ChevronRight } from 'lucide-react';
import type { MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { dayToneClass } from '@/features/schedule/list/days';
import { formatDayHeading } from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import type { DayHeadingProps } from '../slots';

/**
 * Werkstatt's day: a sticky band in the day's tone, its date in bold
 * capitals, with ‹ and › to the headings of the days before and after.
 * Today and tomorrow say so, and today is the current date for assistive
 * technology.
 */
export function DayHeading({ id, date, tone, prevId, nextId }: DayHeadingProps) {
  const { t, i18n } = useTranslation('schedule');
  const heading = formatDayHeading(date, i18n.resolvedLanguage ?? 'en');
  return (
    <div
      className={cn(
        'sticky top-0 z-10 flex items-center gap-1 rounded-[3px] px-1 py-1.5',
        dayToneClass[tone],
      )}
    >
      {prevId && <DayLink target={prevId} label={t('werkstatt.prevDay')} direction="prev" />}
      <h2
        id={id}
        tabIndex={-1}
        aria-current={tone === 'today' ? 'date' : undefined}
        className="min-w-0 flex-1 px-2 text-base font-bold tracking-[1.5px] uppercase outline-none"
      >
        {tone === 'today' || tone === 'tomorrow' ? t(`list.${tone}`, { day: heading }) : heading}
      </h2>
      {nextId && <DayLink target={nextId} label={t('werkstatt.nextDay')} direction="next" />}
    </div>
  );
}

/**
 * A link to another day's heading. The app runs on a hash router in the
 * demo, where following `#id` would change the route: it scrolls to the
 * heading and focuses it instead (no smooth scrolling, so reduced motion
 * needs nothing).
 */
function DayLink({
  target,
  label,
  direction,
}: {
  target: string;
  label: string;
  direction: 'prev' | 'next';
}) {
  const Icon = direction === 'prev' ? ChevronLeft : ChevronRight;
  const go = (event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    const heading = document.getElementById(target);
    heading?.scrollIntoView({ block: 'start' });
    heading?.focus({ preventScroll: true });
  };
  return (
    <a
      href={`#${target}`}
      aria-label={label}
      onClick={go}
      className="inline-flex size-8 shrink-0 items-center justify-center rounded-[3px] outline-none hover:bg-black/10 focus-visible:ring-[3px] focus-visible:ring-ring/50"
    >
      <Icon className="size-5" aria-hidden="true" />
    </a>
  );
}
