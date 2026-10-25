import { describe, expect, it } from 'vitest';
import { personsSearchFrom, personsSearchState } from './personsSearch';

describe('personsSearch', () => {
  it('reads the search this page load wrote', () => {
    expect(personsSearchFrom({ personsSearch: personsSearchState('mia') })).toBe('mia');
  });

  it('ignores the search of an earlier page load, so a reload starts empty', () => {
    expect(personsSearchFrom({ personsSearch: { q: 'mia', load: 'an-earlier-load' } })).toBe('');
  });

  it('is empty without a search or with a broken one', () => {
    expect(personsSearchFrom(undefined)).toBe('');
    expect(personsSearchFrom({})).toBe('');
    expect(personsSearchFrom({ personsSearch: { q: 7, load: personsSearchState('').load } })).toBe(
      '',
    );
  });
});
