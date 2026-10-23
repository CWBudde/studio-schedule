import { describe, expect, it } from 'vitest';
import { lessonBallet, lessonContemporary, lessonWorkshop } from '@/test/mockApi';
import { layoutDay, layoutWeek, lessonInterval, timeAxis, weekDays } from './layout';

const tz = 'Europe/Berlin';

describe('lessonInterval', () => {
  it('uses the school date and wall-clock minutes, not the browser zone', () => {
    expect(lessonInterval(lessonBallet.startsAt, lessonBallet.endsAt, tz)).toEqual({
      date: '2026-10-26',
      start: 18 * 60,
      end: 19 * 60 + 30,
    });
  });

  it.each([
    ['Saturday before the clocks go back', '2026-10-24T16:00:00Z', '2026-10-24'],
    ['Sunday the clocks go back', '2026-10-25T17:00:00Z', '2026-10-25'],
    ['Saturday before the clocks go forward', '2027-03-27T17:00:00Z', '2027-03-27'],
    ['Sunday the clocks go forward', '2027-03-28T16:00:00Z', '2027-03-28'],
  ])('puts 18:00 on the %s at 18:00', (_, startsAt, date) => {
    const end = new Date(Date.parse(startsAt) + 60 * 60_000).toISOString();
    expect(lessonInterval(startsAt, end, tz)).toEqual({ date, start: 18 * 60, end: 19 * 60 });
  });

  it('puts a lesson after midnight in Berlin on the next school date', () => {
    expect(lessonInterval('2026-10-26T23:30:00Z', '2026-10-27T00:30:00Z', tz)).toEqual({
      date: '2026-10-27',
      start: 30,
      end: 90,
    });
  });

  it('clips a lesson that ends after midnight to 24:00', () => {
    expect(lessonInterval('2026-10-26T22:00:00Z', '2026-10-27T01:00:00Z', tz)).toEqual({
      date: '2026-10-26',
      start: 23 * 60,
      end: 24 * 60,
    });
  });

  it('keeps the real length when the wall clock runs backwards (clocks go back)', () => {
    // 02:30 CEST to 02:15 CET is 45 minutes.
    expect(lessonInterval('2026-10-25T00:30:00Z', '2026-10-25T01:15:00Z', tz)).toEqual({
      date: '2026-10-25',
      start: 2 * 60 + 30,
      end: 3 * 60 + 15,
    });
  });
});

describe('layoutDay', () => {
  it('puts overlapping lessons side by side and keeps the others full width', () => {
    const placed = layoutDay([
      { item: 'a', start: 600, end: 690 },
      { item: 'b', start: 660, end: 720 },
      { item: 'c', start: 720, end: 780 },
      { item: 'd', start: 900, end: 960 },
    ]);
    const byItem = Object.fromEntries(placed.map((p) => [p.item, [p.lane, p.lanes]]));
    expect(byItem).toEqual({ a: [0, 2], b: [1, 2], c: [0, 1], d: [0, 1] });
  });

  it('reuses a free lane inside a cluster', () => {
    const placed = layoutDay([
      { item: 'long', start: 600, end: 780 },
      { item: 'first', start: 600, end: 660 },
      { item: 'second', start: 660, end: 720 },
    ]);
    const byItem = Object.fromEntries(placed.map((p) => [p.item, [p.lane, p.lanes]]));
    expect(byItem).toEqual({ long: [0, 2], first: [1, 2], second: [1, 2] });
  });

  it('treats very short lessons as tall enough to read', () => {
    const placed = layoutDay([
      { item: 'a', start: 600, end: 600 },
      { item: 'b', start: 610, end: 640 },
    ]);
    expect(placed.map((p) => p.lanes)).toEqual([2, 2]);
  });
});

describe('timeAxis', () => {
  it('defaults to 08:00–22:00 when there is nothing to show', () => {
    expect(timeAxis([])).toEqual({ startHour: 8, endHour: 22 });
  });

  it('spans from the earliest hour to the latest end, rounded to whole hours', () => {
    expect(
      timeAxis([
        { start: 17 * 60 + 15, end: 18 * 60 },
        { start: 18 * 60, end: 19 * 60 + 30 },
      ]),
    ).toEqual({ startHour: 17, endHour: 20 });
  });

  it('shows at least one hour', () => {
    expect(timeAxis([{ start: 600, end: 600 }])).toEqual({ startHour: 10, endHour: 11 });
  });
});

describe('weekDays', () => {
  it('lists seven dates from the Monday on, across the change of clocks', () => {
    expect(weekDays('2026-10-19')).toEqual([
      '2026-10-19',
      '2026-10-20',
      '2026-10-21',
      '2026-10-22',
      '2026-10-23',
      '2026-10-24',
      '2026-10-25',
    ]);
  });
});

describe('layoutWeek', () => {
  it('groups the week’s lessons by school date and drops the rest', () => {
    const week = layoutWeek([lessonBallet, lessonContemporary, lessonWorkshop], '2026-10-26', tz);

    expect(week.days.map((d) => d.placements.map((p) => p.item.id))).toEqual([
      [lessonBallet.id],
      [lessonContemporary.id],
      [],
      [],
      [],
      [],
      [],
    ]);
    expect(week.axis).toEqual({ startHour: 17, endHour: 20 });
    expect(week.isEmpty).toBe(false);
  });

  it('is empty when no lesson falls into the week', () => {
    const week = layoutWeek([lessonWorkshop], '2026-10-26', tz);
    expect(week.isEmpty).toBe(true);
    expect(week.axis).toEqual({ startHour: 8, endHour: 22 });
  });
});
