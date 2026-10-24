import { describe, expect, it } from 'vitest';
import {
  addDays,
  formatDate,
  formatDateTime,
  formatTimestamp,
  formatDayHeading,
  formatTime,
  formatTimeRange,
  minutesSinceMidnight,
  schoolDateOf,
  startOfWeek,
  todayIn,
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

  it('formats an instant with date and time in the school time zone', () => {
    expect(formatDateTime('2026-10-25T23:30:00Z', berlin, 'de')).toBe('Mo., 26. Okt., 00:30');
  });

  it('formats a timestamp with the year in the school time zone', () => {
    expect(formatTimestamp('2026-10-25T23:30:00Z', berlin, 'en')).toBe('Oct 26, 2026, 00:30');
    expect(formatTimestamp('2026-10-23T14:30:00Z', berlin, 'de')).toBe('23. Okt. 2026, 16:30');
  });
});
