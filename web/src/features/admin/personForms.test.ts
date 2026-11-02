import { describe, expect, it } from 'vitest';
import { personSchema, personValues } from './personForms';

/** The message key `birthDate` fails with when today is `today`, or undefined. */
const birthDate = (value: string, today: string | undefined = '2026-11-02') => {
  const r = personSchema(today).safeParse({
    ...personValues(),
    givenName: 'Paul',
    familyName: 'Sample',
    birthDate: value,
  });
  return r.success ? undefined : r.error.issues.find((i) => i.path[0] === 'birthDate')?.message;
};

describe('personSchema', () => {
  it('takes no birth date or one up to today', () => {
    expect(birthDate('')).toBeUndefined();
    expect(birthDate('2016-02-29')).toBeUndefined();
    expect(birthDate('2026-11-02')).toBeUndefined();
    expect(birthDate('2016-02-29', undefined)).toBeUndefined();
  });

  it('refuses a birth date that does not exist, not one in the future', () => {
    expect(birthDate('2026-02-30')).toBe('date');
    expect(birthDate('2999-02-30')).toBe('date');
    expect(birthDate('2026-02-30', undefined)).toBe('date');
    expect(birthDate('2016-2-3')).toBe('date');
  });

  it('refuses a birth date after today', () => {
    expect(birthDate('2026-11-03')).toBe('birthDateFuture');
  });
});
