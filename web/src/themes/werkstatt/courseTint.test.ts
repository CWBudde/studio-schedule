import { describe, expect, it } from 'vitest';
import { courseTint, courseTints } from './courseTint';

/** A course type id like the API's (UUIDv7), numbered. */
const courseTypeId = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

describe('courseTint', () => {
  it('is stable for an id', () => {
    const id = courseTypeId(31);
    expect(courseTint(id)).toBe(courseTint(id));
    expect(courseTint(id)).toBe(courseTint(`${id}`.slice(0)));
  });

  it('is always one of the palette', () => {
    for (let n = 0; n < 100; n++) expect(courseTints).toContain(courseTint(courseTypeId(n)));
    expect(courseTints).toContain(courseTint(''));
  });

  it('spreads different ids over the palette', () => {
    const used = new Set(Array.from({ length: 30 }, (_, n) => courseTint(courseTypeId(n))));
    expect(used.size).toBeGreaterThanOrEqual(4);
  });
});
