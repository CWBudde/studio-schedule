import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// A fresh module per test: the flag is kept in memory, too.
let hasEnteredDemo: () => boolean;
let markDemoEntered: () => void;

beforeEach(async () => {
  vi.resetModules();
  ({ hasEnteredDemo, markDemoEntered } = await import('./entered'));
});

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.removeItem('studio-schedule-demo-entered');
});

describe('entered', () => {
  it('remembers that the demo was opened', () => {
    expect(hasEnteredDemo()).toBe(false);
    markDemoEntered();
    expect(hasEnteredDemo()).toBe(true);
  });

  it('lets the visitor in for this page load when storage is unavailable', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });

    expect(hasEnteredDemo()).toBe(false);
    expect(() => markDemoEntered()).not.toThrow();
    expect(hasEnteredDemo()).toBe(true);
  });
});
