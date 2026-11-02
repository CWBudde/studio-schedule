import { describe, expect, it } from 'vitest';
import { classMemberData, classMemberSchema } from './classMemberForms';

/** The message key `since` fails with, or undefined. */
const since = (value: string) => {
  const r = classMemberSchema.safeParse({ personId: 'p', since: value });
  return r.success ? undefined : r.error.issues.find((i) => i.path[0] === 'since')?.message;
};

describe('classMemberSchema', () => {
  it('takes no date or one that exists', () => {
    expect(since('')).toBeUndefined();
    expect(since('2028-02-29')).toBeUndefined();
  });

  it('refuses a day that does not exist or no date at all', () => {
    expect(since('2026-02-30')).toBe('date');
    expect(since('2026-2-3')).toBe('date');
  });

  it('leaves an empty date out of the request', () => {
    expect(classMemberData({ personId: 'p', since: '' })).toEqual({ personId: 'p' });
    expect(classMemberData({ personId: 'p', since: '2026-09-01' })).toEqual({
      personId: 'p',
      since: '2026-09-01',
    });
  });
});
