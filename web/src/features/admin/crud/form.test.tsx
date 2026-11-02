import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useAdminErrorText } from './form';

describe('useAdminErrorText', () => {
  it('explains a date that is none', () => {
    const { result } = renderHook(() => useAdminErrorText());
    expect(result.current({ type: 'custom', message: 'date' })).toBe('Enter a date.');
  });
});
