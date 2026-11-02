import { describe, expect, it } from 'vitest';
import { type CloneInput, cloneSchema, switchUnit } from './cloneForms';

// Monday 2026-11-02: the earliest target is today for a day and for a week.
const today = '2026-11-02';

/** The message keys of a clone form's values by field, none when it passes. */
const errorsOf = (values: CloneInput) => {
  const r = cloneSchema(today).safeParse(values);
  return r.success ? {} : Object.fromEntries(r.error.issues.map((i) => [i.path[0], i.message]));
};

describe('cloneSchema', () => {
  it('refuses a source or target that does not exist', () => {
    expect(errorsOf({ unit: 'day', source: '2026-02-30', target: '2026-11-10' })).toEqual({
      source: 'date',
    });
    expect(errorsOf({ unit: 'day', source: '2026-11-03', target: '2026-11-31' })).toEqual({
      target: 'date',
    });
    // 2026-02-30 would roll over to Monday 2026-03-02: still no date.
    expect(errorsOf({ unit: 'week', source: '2026-02-30', target: '2026-11-09' })).toEqual({
      source: 'date',
    });
  });

  it('accepts dates that exist', () => {
    expect(errorsOf({ unit: 'day', source: '2026-11-03', target: '2026-11-10' })).toEqual({});
    expect(errorsOf({ unit: 'week', source: '2026-03-02', target: '2026-11-09' })).toEqual({});
  });
});

describe('switchUnit', () => {
  it('snaps dates that exist to their Monday for a week', () => {
    expect(
      switchUnit({ unit: 'day', source: '2026-11-04', target: '2026-11-12' }, 'week', today),
    ).toEqual({ unit: 'week', source: '2026-11-02', target: '2026-11-09' });
  });

  it('keeps a date that does not exist as typed', () => {
    expect(
      switchUnit({ unit: 'day', source: '2026-02-30', target: '2026-11-31' }, 'week', today),
    ).toEqual({ unit: 'week', source: '2026-02-30', target: '2026-11-31' });
  });
});
