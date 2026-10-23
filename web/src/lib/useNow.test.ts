import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useNow, useServerNow } from './useNow';

const start = new Date('2026-10-23T10:00:00Z').getTime();

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(start);
});

afterEach(() => {
  vi.useRealTimers();
});

describe('useNow', () => {
  it('ticks once per interval', () => {
    const { result } = renderHook(() => useNow(60_000));
    expect(result.current).toBe(start);

    act(() => vi.advanceTimersByTime(59_000));
    expect(result.current).toBe(start);

    act(() => vi.advanceTimersByTime(1_000));
    expect(result.current).toBe(start + 60_000);
  });

  it('refreshes when the page becomes visible again', () => {
    const { result } = renderHook(() => useNow(60_000));

    vi.setSystemTime(start + 8 * 60 * 60 * 1000);
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'));
    });

    expect(result.current).toBe(start + 8 * 60 * 60 * 1000);
  });
});

describe('useServerNow', () => {
  it('corrects the client clock by the offset at the response', () => {
    // The server is 2 hours behind the browser; the response arrived 5 minutes ago.
    const receivedAt = start - 5 * 60 * 1000;
    const serverNow = new Date(receivedAt - 2 * 60 * 60 * 1000).toISOString();

    const { result } = renderHook(() => useServerNow(serverNow, receivedAt));

    expect(result.current).toBe(start - 2 * 60 * 60 * 1000);
  });
});
