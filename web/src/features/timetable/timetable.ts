import type { components } from '@/api/schema';
import { addDays, formatDate, type SchoolDate } from '@/lib/schoolTime';

export type Term = components['schemas']['Term'];
export type WeeklyClass = components['schemas']['Class'];

/**
 * The term to show when none is chosen: the one that contains today, else
 * the next one to start, else the one that started last.
 */
export function defaultTerm(terms: readonly Term[], today: SchoolDate): Term | undefined {
  const byStart = [...terms].sort((a, b) => a.startsOn.localeCompare(b.startsOn));
  return (
    byStart.find((t) => t.startsOn <= today && today <= t.endsOn) ??
    byStart.find((t) => t.startsOn > today) ??
    byStart.at(-1)
  );
}

/** The wall-clock time `minutes` after the time of day `start` ('HH:MM'). */
export function endTime(start: string, minutes: number): string {
  const [h = 0, m = 0] = start.split(':').map(Number);
  const total = (((h * 60 + m + minutes) % 1440) + 1440) % 1440;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(Math.floor(total / 60))}:${pad(total % 60)}`;
}

export type WeekdayGroup = { weekday: number; classes: WeeklyClass[] };

/** Position of an API weekday (0 = Sunday) in a week that starts on Monday. */
const mondayFirst = (weekday: number) => (weekday + 6) % 7;

/**
 * Groups classes by weekday, Monday first and Sunday last, each day's
 * classes by start time. Days without classes are left out.
 */
export function groupByWeekday(classes: readonly WeeklyClass[]): WeekdayGroup[] {
  const days = new Map<number, WeeklyClass[]>();
  for (const c of classes) {
    const list = days.get(c.weekday);
    if (list) list.push(c);
    else days.set(c.weekday, [c]);
  }
  return Array.from(days, ([weekday, list]) => ({
    weekday,
    classes: list.sort((a, b) => a.startTime.localeCompare(b.startTime)),
  })).sort((a, b) => mondayFirst(a.weekday) - mondayFirst(b.weekday));
}

/** A Sunday, to name weekdays by a school date. */
const aSunday: SchoolDate = '2026-01-04';

/** The name of an API weekday (0 = Sunday) in the UI language, e.g. 'Monday'. */
export function weekdayName(weekday: number, locale: string): string {
  return formatDate(addDays(aSunday, weekday), locale, { weekday: 'long' });
}

/**
 * The first and last day of a term in the UI language, the year only once
 * when both are in the same year: { from: 'Feb 1', to: 'Jul 16, 2027' }.
 */
export function termRange(term: Term, locale: string): { from: string; to: string } {
  const withYear = { day: 'numeric', month: 'short', year: 'numeric' } as const;
  const sameYear = term.startsOn.slice(0, 4) === term.endsOn.slice(0, 4);
  return {
    from: formatDate(
      term.startsOn,
      locale,
      sameYear ? { day: 'numeric', month: 'short' } : withYear,
    ),
    to: formatDate(term.endsOn, locale, withYear),
  };
}
