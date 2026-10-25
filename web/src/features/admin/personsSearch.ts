/**
 * The persons page keeps its search in the browser's history state, not in
 * the URL (ASVS 8.3.1): Back and Forward restore it, and the term never
 * reaches an address, a proxy log or a bookmark. Browsers keep history state
 * across a reload, so the state carries the token of the page load that wrote
 * it, and a term of an earlier load is ignored: a reload starts empty.
 */
declare module '@tanstack/react-router' {
  interface HistoryState {
    /** The persons page's search (personsSearchState). */
    personsSearch?: { q: string; load: string };
  }
}

/** This page load's token; a new load (a reload) makes a new one. */
const pageLoad = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;

/** The state that keeps the search q for this page load. */
export const personsSearchState = (q: string) => ({ q, load: pageLoad });

/** The search a history entry's state keeps: '' without one or from an earlier page load. */
export function personsSearchFrom(state: { personsSearch?: unknown } | undefined): string {
  const kept = state?.personsSearch as { q?: unknown; load?: unknown } | undefined;
  return kept?.load === pageLoad && typeof kept.q === 'string' ? kept.q : '';
}
