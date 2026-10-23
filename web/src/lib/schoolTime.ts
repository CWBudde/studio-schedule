/**
 * Dates and times of the school. The school has one IANA time zone (from the
 * settings); every lesson time is shown in it, whatever the browser's time
 * zone is. Nothing here uses the browser's local time.
 *
 * A school date is a calendar date without a zone, written 'YYYY-MM-DD' as in
 * the API. Calendar arithmetic on school dates runs in UTC, where every day
 * has 24 hours.
 */

/** A calendar date in the form 'YYYY-MM-DD'. */
export type SchoolDate = string;

type Instant = string | number | Date;

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(locale: string, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = `${locale}|${JSON.stringify(options)}`;
  let f = formatters.get(key);
  if (!f) {
    f = new Intl.DateTimeFormat(locale, options);
    formatters.set(key, f);
  }
  return f;
}

function parts(instant: Instant, timeZone: string) {
  const values: Partial<Record<Intl.DateTimeFormatPartTypes, string>> = {};
  for (const p of formatter('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(new Date(instant))) {
    values[p.type] = p.value;
  }
  return values;
}

/** The school date on which the instant falls in the school time zone. */
export function schoolDateOf(instant: Instant, timeZone: string): SchoolDate {
  const p = parts(instant, timeZone);
  return `${p.year}-${p.month}-${p.day}`;
}

/** Today's date in the school time zone. */
export function todayIn(timeZone: string, now: Instant = Date.now()): SchoolDate {
  return schoolDateOf(now, timeZone);
}

/** Minutes after midnight of the instant's wall-clock time in the school time zone. */
export function minutesSinceMidnight(instant: Instant, timeZone: string): number {
  const p = parts(instant, timeZone);
  return Number(p.hour) * 60 + Number(p.minute);
}

function utcDate(date: SchoolDate): Date {
  const [y = 0, m = 1, d = 1] = date.split('-').map(Number);
  return new Date(Date.UTC(y, m - 1, d));
}

/** The date n calendar days after date (before it if n < 0). */
export function addDays(date: SchoolDate, n: number): SchoolDate {
  const t = utcDate(date);
  t.setUTCDate(t.getUTCDate() + n);
  return t.toISOString().slice(0, 10);
}

/** Day of the week, 0 = Sunday to 6 = Saturday (as in the API). */
export function weekdayOf(date: SchoolDate): number {
  return utcDate(date).getUTCDay();
}

/** The Monday of the week that contains date. */
export function startOfWeek(date: SchoolDate): SchoolDate {
  return addDays(date, -((weekdayOf(date) + 6) % 7));
}

/** Formats a school date in the UI language, e.g. { weekday: 'short' } → 'Mon'. */
export function formatDate(
  date: SchoolDate,
  locale: string,
  options: Intl.DateTimeFormatOptions,
): string {
  return formatter(locale, { ...options, timeZone: 'UTC' }).format(utcDate(date));
}

/** A day heading such as 'Monday, October 26' or 'Montag, 26. Oktober'. */
export function formatDayHeading(date: SchoolDate, locale: string): string {
  return formatDate(date, locale, { weekday: 'long', day: 'numeric', month: 'long' });
}

/** The wall-clock time of the instant in the school time zone, e.g. '18:00'. */
export function formatTime(instant: Instant, timeZone: string, locale: string): string {
  return formatter(locale, {
    timeZone,
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(instant));
}

/** A time range such as '18:00–19:30' in the school time zone. */
export function formatTimeRange(
  start: Instant,
  end: Instant,
  timeZone: string,
  locale: string,
): string {
  return `${formatTime(start, timeZone, locale)}–${formatTime(end, timeZone, locale)}`;
}

/** Date and time of the instant in the school time zone, e.g. 'Mon, Oct 26, 00:30'. */
export function formatDateTime(instant: Instant, timeZone: string, locale: string): string {
  return formatter(locale, {
    timeZone,
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).format(new Date(instant));
}
