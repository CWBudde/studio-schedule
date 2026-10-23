import { createHashHistory, type RouterHistory } from '@tanstack/react-router';

/**
 * Hash history for the static demo. TanStack's hash history appends the
 * page's real query string (before the '#') to the route's search, so on
 * '/?fbclid=x#/schedule?view=week' the last param reads 'week?fbclid=x' and
 * is dropped as invalid. Routes live in the hash only, so the real query
 * string carries nothing for the app: remove it before the history starts.
 */
export function createAppHashHistory(): RouterHistory {
  const { location, history } = window;
  if (location.search) {
    history.replaceState(history.state, '', `${location.pathname}${location.hash}`);
  }
  return createHashHistory();
}
