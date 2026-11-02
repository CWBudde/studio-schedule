import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { personSchema, usePersonErrorText } from './personForm';

const person = { givenName: 'Mara', familyName: 'Lind', phone: '' };

/** The message keys a birth date fails with, by field path. */
const birthDateErrors = (birthDate: string, today?: string) => {
  const r = personSchema(today).safeParse({ ...person, birthDate });
  return r.success ? [] : r.error.issues.map((i) => [i.path.join('.'), i.message]);
};

describe('personSchema', () => {
  it('accepts an existing birth date and none', () => {
    expect(birthDateErrors('2016-02-29', '2026-11-02')).toEqual([]);
    expect(birthDateErrors('', '2026-11-02')).toEqual([]);
    expect(birthDateErrors('2016-02-29')).toEqual([]);
  });

  it('refuses a birth date that does not exist, as a date error only', () => {
    expect(birthDateErrors('2026-02-30', '2026-11-02')).toEqual([['birthDate', 'date']]);
    expect(birthDateErrors('2015-02-29', '2026-11-02')).toEqual([['birthDate', 'date']]);
    // After today as well, but not a date: no birthDateFuture.
    expect(birthDateErrors('2099-02-30', '2026-11-02')).toEqual([['birthDate', 'date']]);
    expect(birthDateErrors('2026-02-30')).toEqual([['birthDate', 'date']]);
  });

  it('refuses a birth date after today', () => {
    expect(birthDateErrors('2026-11-03', '2026-11-02')).toEqual([['birthDate', 'birthDateFuture']]);
  });
});

describe('usePersonErrorText', () => {
  it('translates the date and future keys', () => {
    const { result } = renderHook(() => usePersonErrorText());
    expect(result.current({ type: 'custom', message: 'date' })).toBe('Enter a date.');
    expect(result.current({ type: 'custom', message: 'birthDateFuture' })).toBe(
      'The birth date cannot be in the future.',
    );
    expect(result.current({ type: 'custom', message: 'required' })).toBeTruthy();
  });
});
