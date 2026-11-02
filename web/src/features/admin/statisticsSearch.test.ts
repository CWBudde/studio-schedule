import { describe, expect, it } from 'vitest';
import { validateStatisticsSearch } from './statisticsSearch';

describe('validateStatisticsSearch', () => {
  it('keeps a custom range of dates that exist', () => {
    expect(
      validateStatisticsSearch({ range: 'custom', from: '2028-02-29', to: '2028-03-31' }),
    ).toEqual({ range: 'custom', from: '2028-02-29', to: '2028-03-31' });
  });

  it('drops a date that does not exist and what is no date or range', () => {
    expect(
      validateStatisticsSearch({ range: 'custom', from: '2026-02-30', to: '2026-04-31' }),
    ).toEqual({ range: 'custom' });
    expect(validateStatisticsSearch({ range: 'decade', from: 20260101, to: '2026-1-1' })).toEqual(
      {},
    );
  });
});
