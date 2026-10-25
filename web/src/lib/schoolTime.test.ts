import { describe, expect, it } from 'vitest';
import {
  addDays,
  formatDate,
  formatDateTime,
  formatTimestamp,
  formatDayHeading,
  formatTime,
  formatTimeRange,
  instantAt,
  minutesSinceMidnight,
  schoolDateOf,
  startOfWeek,
  todayIn,
  wallClockOf,
  weekdayOf,
} from './schoolTime';

const berlin = 'Europe/Berlin';

// The tests run with the browser in New York (vite.config.ts sets TZ), the
// school is in Berlin: nothing may depend on the browser's time zone.

describe('schoolDateOf', () => {
  it('returns the school date, not the UTC or browser date', () => {
    // 00:30 in Berlin on Monday is still Sunday in UTC and in New York.
    expect(schoolDateOf('2026-10-25T23:30:00Z', berlin)).toBe('2026-10-26');
    expect(schoolDateOf(new Date('2026-10-25T21:59:00Z'), berlin)).toBe('2026-10-25');
  });

  it('handles both DST changes', () => {
    // Clocks go back on 2026-10-25 (CEST → CET) and forward on 2027-03-28.
    expect(schoolDateOf('2026-10-24T22:00:00Z', berlin)).toBe('2026-10-25');
    expect(schoolDateOf('2026-10-25T22:59:00Z', berlin)).toBe('2026-10-25');
    expect(schoolDateOf('2026-10-25T23:00:00Z', berlin)).toBe('2026-10-26');
    expect(schoolDateOf('2027-03-27T23:00:00Z', berlin)).toBe('2027-03-28');
    expect(schoolDateOf('2027-03-28T21:59:00Z', berlin)).toBe('2027-03-28');
    expect(schoolDateOf('2027-03-28T22:00:00Z', berlin)).toBe('2027-03-29');
  });
});

describe('calendar arithmetic', () => {
  it('adds days across month, year and DST boundaries', () => {
    expect(addDays('2026-10-24', 1)).toBe('2026-10-25');
    expect(addDays('2026-10-25', 1)).toBe('2026-10-26');
    expect(addDays('2026-12-31', 1)).toBe('2027-01-01');
    expect(addDays('2027-03-01', -1)).toBe('2027-02-28');
    expect(addDays('2026-10-23', 14)).toBe('2026-11-06');
  });

  it('knows weekdays and the Monday of a week', () => {
    expect(weekdayOf('2026-10-25')).toBe(0); // Sunday
    expect(weekdayOf('2026-10-26')).toBe(1);
    expect(startOfWeek('2026-10-25')).toBe('2026-10-19');
    expect(startOfWeek('2026-10-26')).toBe('2026-10-26');
    expect(startOfWeek('2026-10-23')).toBe('2026-10-19');
  });

  it('returns today in the school time zone', () => {
    expect(todayIn(berlin, new Date('2026-10-25T23:30:00Z'))).toBe('2026-10-26');
  });
});

describe('formatting', () => {
  it('formats times in the school time zone across DST', () => {
    // An 18:00 class before and after clocks go back.
    expect(formatTime('2026-10-24T16:00:00Z', berlin, 'de')).toBe('18:00');
    expect(formatTime('2026-10-25T17:00:00Z', berlin, 'de')).toBe('18:00');
    expect(formatTime('2027-03-28T16:00:00Z', berlin, 'de')).toBe('18:00');
    expect(formatTime('2026-10-24T16:00:00Z', berlin, 'en')).toBe('18:00');
  });

  it('formats a time range', () => {
    expect(formatTimeRange('2026-10-24T16:00:00Z', '2026-10-24T17:30:00Z', berlin, 'en')).toBe(
      '18:00–19:30',
    );
  });

  it('gives minutes since school midnight', () => {
    expect(minutesSinceMidnight('2026-10-25T17:00:00Z', berlin)).toBe(18 * 60);
    expect(minutesSinceMidnight('2026-10-25T23:30:00Z', berlin)).toBe(30);
  });

  it('formats day headings and dates in the UI language', () => {
    expect(formatDayHeading('2026-10-26', 'en')).toBe('Monday, October 26');
    expect(formatDayHeading('2026-10-26', 'de')).toBe('Montag, 26. Oktober');
    expect(formatDate('2026-10-26', 'en', { weekday: 'short' })).toBe('Mon');
  });

  it('adds the year to a day heading outside the year of today', () => {
    expect(formatDayHeading('2026-12-28', 'en', '2026-10-25')).toBe('Monday, December 28');
    expect(formatDayHeading('2027-01-04', 'en', '2026-10-25')).toBe('Monday, January 4, 2027');
    expect(formatDayHeading('2027-01-04', 'de', '2026-10-25')).toBe('Montag, 4. Januar 2027');
  });

  it('formats an instant with date and time in the school time zone', () => {
    expect(formatDateTime('2026-10-25T23:30:00Z', berlin, 'de')).toBe('Mo., 26. Okt., 00:30');
  });

  it('formats a timestamp with the year in the school time zone', () => {
    expect(formatTimestamp('2026-10-25T23:30:00Z', berlin, 'en')).toBe('Oct 26, 2026, 00:30');
    expect(formatTimestamp('2026-10-23T14:30:00Z', berlin, 'de')).toBe('23. Okt. 2026, 16:30');
  });
});

describe('instantAt', () => {
  it('turns a school date and time into the instant, whatever the browser zone', () => {
    expect(instantAt('2026-10-21', '18:00', berlin)).toBe('2026-10-21T16:00:00.000Z');
    expect(instantAt('2026-12-01', '09:30', berlin)).toBe('2026-12-01T08:30:00.000Z');
    expect(wallClockOf('2026-12-01T08:30:00Z', berlin)).toBe('09:30');
  });

  it('handles the days the clocks change', () => {
    // Back on 2026-10-25 at 03:00 to 02:00; forward on 2027-03-28 at 02:00.
    expect(instantAt('2026-10-25', '12:00', berlin)).toBe('2026-10-25T11:00:00.000Z');
    expect(instantAt('2027-03-28', '12:00', berlin)).toBe('2027-03-28T10:00:00.000Z');
    expect(instantAt('2027-03-28', '00:30', berlin)).toBe('2027-03-27T23:30:00.000Z');
  });

  it('takes the first of a time that occurs twice when the clocks go back', () => {
    // 02:30 is 00:30Z in summer time (+2) and 01:30Z in winter time (+1).
    expect(instantAt('2026-10-25', '02:30', berlin)).toBe('2026-10-25T00:30:00.000Z');
    expect(instantAt('2026-10-25', '02:00', berlin)).toBe('2026-10-25T00:00:00.000Z');
    // Around the repeated hour, the one time each.
    expect(instantAt('2026-10-25', '01:59', berlin)).toBe('2026-10-24T23:59:00.000Z');
    expect(instantAt('2026-10-25', '03:00', berlin)).toBe('2026-10-25T02:00:00.000Z');
  });

  it('moves a time the clocks skip past the change', () => {
    // 02:00 to 03:00 does not exist on 2027-03-28: 02:30 counts as 03:30
    // summer time, the instant 02:30 winter time would be.
    expect(instantAt('2027-03-28', '02:30', berlin)).toBe('2027-03-28T01:30:00.000Z');
    expect(wallClockOf('2027-03-28T01:30:00.000Z', berlin)).toBe('03:30');
    expect(instantAt('2027-03-28', '03:00', berlin)).toBe('2027-03-28T01:00:00.000Z');
    expect(instantAt('2027-03-28', '01:59', berlin)).toBe('2027-03-28T00:59:00.000Z');
  });
});
