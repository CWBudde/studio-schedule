import { afterEach, describe, expect, it, vi } from 'vitest';
import { cachedTheme, cacheTheme, THEME_CACHE_KEY } from './themeCache';

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.removeItem(THEME_CACHE_KEY);
});

describe('themeCache', () => {
  it('has no theme before one is cached', () => {
    expect(cachedTheme()).toBeUndefined();
  });

  it('returns the cached theme', () => {
    cacheTheme('werkstatt');

    expect(localStorage.getItem(THEME_CACHE_KEY)).toBe('werkstatt');
    expect(cachedTheme()).toBe('werkstatt');
  });

  it('ignores a value that is no theme', () => {
    localStorage.setItem(THEME_CACHE_KEY, 'neon');

    expect(cachedTheme()).toBeUndefined();
  });

  it('works without storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('denied', 'SecurityError');
    });

    expect(cachedTheme()).toBeUndefined();
    expect(() => cacheTheme('programme')).not.toThrow();
  });
});
