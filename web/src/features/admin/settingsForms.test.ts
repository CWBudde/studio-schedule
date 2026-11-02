import { describe, expect, it } from 'vitest';
import { type RulesValues, rulesSchemaUntil } from './settingsForms';

const rules: RulesValues = {
  releaseWeekday: '1',
  releaseTime: '08:00',
  releaseWeeksAhead: '4',
  releasedThrough: '',
  bookingCutoff: '2',
  bookingCutoffUnit: 'hours',
  cancellationCutoff: '24',
  cancellationCutoffUnit: 'hours',
  waitlistInviteExpiry: '12',
  waitlistInviteExpiryUnit: 'hours',
  defaultVisibleDays: '28',
};

/** The message key `releasedThrough` fails with, or undefined. */
const releasedThrough = (value: string, latest?: string) => {
  const r = rulesSchemaUntil(latest).safeParse({ ...rules, releasedThrough: value });
  return r.success
    ? undefined
    : r.error.issues.find((i) => i.path[0] === 'releasedThrough')?.message;
};

describe('rulesSchemaUntil', () => {
  it('takes no released-through date or one that exists, up to the latest', () => {
    expect(releasedThrough('')).toBeUndefined();
    expect(releasedThrough('2026-12-31', '2027-11-01')).toBeUndefined();
    expect(releasedThrough('2027-11-02', '2027-11-01')).toBe('releasedThroughLatest');
  });

  it('refuses a released-through date that does not exist', () => {
    expect(releasedThrough('2026-02-30')).toBe('date');
    expect(releasedThrough('2026-02-30', '2027-11-01')).toBe('date');
  });
});
