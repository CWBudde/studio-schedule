import type { ThemeId } from './slots';

/** Where the last theme of the school is kept, for the next start (web/public/theme.js reads it too). */
export const THEME_CACHE_KEY = 'studio-schedule-theme';

const known: readonly string[] = ['programme', 'werkstatt'] satisfies ThemeId[];

/** The cached theme, or undefined without one (or without storage). */
export function cachedTheme(): ThemeId | undefined {
  try {
    const value = localStorage.getItem(THEME_CACHE_KEY);
    return value !== null && known.includes(value) ? (value as ThemeId) : undefined;
  } catch {
    return undefined;
  }
}

/** Keeps the theme for the next start; without storage it is not kept. */
export function cacheTheme(id: ThemeId): void {
  try {
    localStorage.setItem(THEME_CACHE_KEY, id);
  } catch {
    // Storage may be unavailable (a private window); the next start shows Programme first.
  }
}
