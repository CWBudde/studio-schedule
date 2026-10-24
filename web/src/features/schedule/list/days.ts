import { type SchoolDate, schoolDateOf } from '@/lib/schoolTime';

/** How a day heading is colored, by the day's distance from today. */
export type DayTone = 'past' | 'today' | 'tomorrow' | 'later';

export const dayToneClass: Record<DayTone, string> = {
  past: 'bg-muted text-foreground',
  today: 'bg-day-today text-day-today-foreground',
  tomorrow: 'bg-day-tomorrow text-day-tomorrow-foreground',
  later: 'bg-day-later text-day-later-foreground',
};

export function dayTone(date: SchoolDate, today: SchoolDate, tomorrow: SchoolDate): DayTone {
  if (date < today) return 'past';
  if (date === today) return 'today';
  return date === tomorrow ? 'tomorrow' : 'later';
}

/** The lessons of one school date. */
export type Day<L> = { date: SchoolDate; lessons: L[] };

/** Groups lessons by the school date they start on, keeping their order. */
export function groupByDay<L extends { startsAt: string }>(
  lessons: readonly L[],
  timeZone: string,
): Day<L>[] {
  const days = new Map<SchoolDate, L[]>();
  for (const lesson of lessons) {
    const date = schoolDateOf(lesson.startsAt, timeZone);
    const list = days.get(date);
    if (list) list.push(lesson);
    else days.set(date, [lesson]);
  }
  return Array.from(days, ([date, list]) => ({ date, lessons: list }));
}
