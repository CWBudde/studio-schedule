import { describe, expect, it } from 'vitest';
import type { ClassEntry } from '@/api/queries';
import { ageMonths, classValues } from './classForms';

/** The months of an age as typed, or the message key it fails with. */
const months = (years: string) => {
  const r = ageMonths.safeParse(years);
  return r.success ? r.data : r.error.issues[0]?.message;
};

/** A class as listed, with only its ages filled in. */
const classWith = (ages: Pick<ClassEntry, 'ageFromMonths' | 'ageToMonths'>) =>
  ({ instructor: { id: '' }, ...ages }) as ClassEntry;

describe('ageMonths', () => {
  it('reads whole and half years, with a point or a comma', () => {
    expect(months('')).toBeUndefined();
    expect(months('4')).toBe(48);
    expect(months('3.5')).toBe(42);
    expect(months('3,5')).toBe(42);
    expect(months('0.5')).toBe(6);
    expect(months('100')).toBe(1200);
  });

  it('refuses what is no whole number of months from 1 to 1200', () => {
    for (const years of ['0', '3.1', '100.5', 'abc', '-1']) expect(months(years)).toBe('age');
  });

  it('keeps ages the API stored in other months than half years', () => {
    // 40 months is 3.333… years: the form holds '3.33' and sends 40 again.
    for (const stored of [1, 40, 43, 1199]) {
      const { ageFrom, ageTo } = classValues(
        classWith({ ageFromMonths: stored, ageToMonths: stored }),
      );
      expect([months(ageFrom), months(ageTo)]).toEqual([stored, stored]);
    }
    expect(classValues(classWith({ ageFromMonths: 40 })).ageFrom).toBe('3.33');
  });
});
