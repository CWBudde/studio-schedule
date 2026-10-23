import { describe, expect, it } from 'vitest';
import { autumnClasses, ids, springClasses, terms } from '@/test/mockApi';
import { defaultTerm, endTime, groupByWeekday, termRange, weekdayName } from './timetable';

describe('defaultTerm', () => {
  it('picks the term that contains today', () => {
    expect(defaultTerm(terms, '2026-10-23')?.id).toBe(ids.termAutumn);
    // Both ends are inclusive.
    expect(defaultTerm(terms, '2026-08-31')?.id).toBe(ids.termAutumn);
    expect(defaultTerm(terms, '2027-01-29')?.id).toBe(ids.termAutumn);
  });

  it('picks the next upcoming term between terms', () => {
    expect(defaultTerm(terms, '2026-08-01')?.id).toBe(ids.termAutumn);
    expect(defaultTerm(terms, '2027-01-30')?.id).toBe(ids.termSpring);
  });

  it('picks the earliest upcoming term whatever the order of the list', () => {
    expect(defaultTerm([...terms].reverse(), '2025-01-01')?.id).toBe(ids.termPast);
  });

  it('picks the latest term after all terms have ended', () => {
    expect(defaultTerm([...terms].reverse(), '2028-01-01')?.id).toBe(ids.termSpring);
  });

  it('has no term without terms', () => {
    expect(defaultTerm([], '2026-10-23')).toBeUndefined();
  });
});

describe('endTime', () => {
  it('adds the duration to the start time', () => {
    expect(endTime('18:00', 90)).toBe('19:30');
    expect(endTime('09:45', 15)).toBe('10:00');
    expect(endTime('23:30', 45)).toBe('00:15');
  });
});

describe('groupByWeekday', () => {
  it('starts the week on Monday and puts Sunday last', () => {
    const days = groupByWeekday(autumnClasses);
    expect(days.map((d) => d.weekday)).toEqual([1, 2, 0]);
    expect(days[0]?.classes.map((c) => c.id)).toEqual([ids.classBallet]);
    expect(days[2]?.classes.map((c) => c.id)).toEqual([ids.classJazz]);
  });

  it('orders the classes of a day by start time and leaves out empty days', () => {
    const [ballet] = autumnClasses.filter((c) => c.weekday === 1);
    if (!ballet) throw new Error('fixture');
    const early = { ...ballet, id: 'early', startTime: '09:00' };
    const days = groupByWeekday([...springClasses, ballet, early]);
    expect(days.map((d) => d.weekday)).toEqual([1, 4]);
    expect(days[0]?.classes.map((c) => c.id)).toEqual(['early', ids.classBallet]);
  });
});

describe('weekdayName', () => {
  it('names the API weekday (0 = Sunday) in the UI language', () => {
    expect(weekdayName(0, 'en')).toBe('Sunday');
    expect(weekdayName(1, 'en')).toBe('Monday');
    expect(weekdayName(6, 'de')).toBe('Samstag');
  });
});

describe('termRange', () => {
  it('names the year once when the term starts and ends in the same year', () => {
    const [past, autumn, spring] = terms;
    if (!past || !autumn || !spring) throw new Error('fixture');
    expect(termRange(spring, 'en')).toEqual({ from: 'Feb 1', to: 'Jul 16, 2027' });
    expect(termRange(autumn, 'en')).toEqual({ from: 'Aug 31, 2026', to: 'Jan 29, 2027' });
    expect(termRange(spring, 'de')).toEqual({ from: '1. Feb.', to: '16. Juli 2027' });
  });
});
