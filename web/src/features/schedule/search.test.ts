import { describe, expect, it } from 'vitest';
import { validateScheduleSearch } from './search';

describe('validateScheduleSearch', () => {
  it('keeps a first date that exists', () => {
    expect(validateScheduleSearch({ from: '2026-11-02' })).toEqual({ from: '2026-11-02' });
  });

  it('drops a first date that does not exist', () => {
    expect(validateScheduleSearch({ from: '2026-02-30', view: 'week' })).toEqual({
      view: 'week',
    });
  });
});
