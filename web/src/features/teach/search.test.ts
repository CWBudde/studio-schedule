import { describe, expect, it } from 'vitest';
import { validateTeachSearch } from './search';

describe('validateTeachSearch', () => {
  it('keeps a first date that exists', () => {
    expect(validateTeachSearch({ from: '2026-11-02' })).toEqual({ from: '2026-11-02' });
  });

  it('drops a first date that does not exist', () => {
    expect(validateTeachSearch({ from: '2026-02-30' })).toEqual({});
  });
});
