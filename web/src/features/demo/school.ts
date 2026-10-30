import { routerHistory } from '@/lib/env';
import { markDemoEntered } from './entered';

// The web demo offers two fictional schools, one per theme
// (docs/demo-mode.md, "Two demo schools"). The visitor picks one with
// `?demo=` or on the about page; the choice is kept in localStorage, which
// can be unavailable (private windows, blocked site data): the choice then
// holds for this page load only.

/** A demo school; the worker seeds it (`seed.School` on the Go side). */
export type DemoSchool = 'programme' | 'werkstatt';

export const DEMO_SCHOOL_KEY = 'studio-schedule-demo-school';

const DEFAULT_SCHOOL: DemoSchool = 'programme';

let chosen: DemoSchool | undefined;

function isDemoSchool(value: unknown): value is DemoSchool {
  return value === 'programme' || value === 'werkstatt';
}

/** The school the visitor chose, or programme. */
export function demoSchool(): DemoSchool {
  if (chosen) return chosen;
  try {
    const stored = localStorage.getItem(DEMO_SCHOOL_KEY);
    return isDemoSchool(stored) ? stored : DEFAULT_SCHOOL;
  } catch {
    return DEFAULT_SCHOOL;
  }
}

/** Remembers the school for this page load and, if storage allows, later visits. */
export function chooseDemoSchool(s: DemoSchool): void {
  chosen = s;
  try {
    localStorage.setItem(DEMO_SCHOOL_KEY, s);
  } catch {
    // The next visit opens the default school again.
  }
}

/**
 * The school named by `?demo=` in `url` (outside the hash), if any. The
 * parameter is removed from the address, a value that names no school too.
 */
export function takeDemoParam(url: URL): DemoSchool | undefined {
  if (!url.searchParams.has('demo')) return undefined;
  const value = url.searchParams.get('demo');
  url.searchParams.delete('demo');
  history.replaceState(history.state, '', url);
  return isDemoSchool(value) ? value : undefined;
}

/**
 * Opens the demo with the other school: stores the choice and reloads at the
 * home page, so a new worker seeds or restores that school's data.
 */
export function switchDemoSchool(s: DemoSchool, reload = () => window.location.reload()): void {
  chooseDemoSchool(s);
  markDemoEntered();
  const home = new URL(import.meta.env.BASE_URL, window.location.href);
  if (routerHistory === 'hash') home.hash = '/';
  history.replaceState(null, '', home);
  reload();
}
