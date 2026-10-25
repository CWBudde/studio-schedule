import type { Query } from '@tanstack/react-query';
import type {
  PersistedClient,
  Persister,
  PersistQueryClientOptions,
} from '@tanstack/react-query-persist-client';
import { createStore, del, get, set } from 'idb-keyval';

/**
 * The offline cache (ADR 0017): the last public schedule and the family's
 * coming lessons survive a reload in IndexedDB, so the app shows them
 * offline, read-only. Only the queries of the allow-list are kept, never
 * the API's other answers, and the copy is deleted when the account signs
 * out or its session ends (a 401). The service worker caches no API
 * answer; they stay `no-store`.
 */

/** How long a saved cache is used: a week. */
export const OFFLINE_CACHE_MAX_AGE = 7 * 24 * 60 * 60 * 1000;

/**
 * Whether the query of this key is kept offline: the public settings, the
 * schedule (every range), the lessons opened from it, My lessons' coming
 * lessons and who is signed in.
 */
export function isPersistedKey(key: readonly unknown[]): boolean {
  const [area, sub] = key;
  return (
    (area === 'settings' && sub === 'public') ||
    area === 'schedule' ||
    area === 'lesson' ||
    (area === 'my-lessons' && sub === 'upcoming') ||
    (area === 'auth' && sub === 'me')
  );
}

/** Whether a query is saved: one of the allow-list with data. */
export function shouldPersistQuery(query: Query): boolean {
  return query.state.status === 'success' && isPersistedKey(query.queryKey);
}

const STORE_DB = 'studio-schedule-offline';
const STORE_NAME = 'query-cache';
const CACHE_KEY = 'queries';

/** A persister that keeps the cache in IndexedDB (idb-keyval). */
export function indexedDbPersister(): Persister {
  const store = createStore(STORE_DB, STORE_NAME);
  return {
    persistClient: (client: PersistedClient) => set(CACHE_KEY, client, store),
    restoreClient: () => get<PersistedClient>(CACHE_KEY, store),
    removeClient: () => del(CACHE_KEY, store),
  };
}

let active: Persister | null = null;

/** The options of the app's PersistQueryClientProvider; `buster` drops the copy of another API version. */
export function offlineCacheOptions(
  persister: Persister,
  buster: string,
): Omit<PersistQueryClientOptions, 'queryClient'> {
  active = persister;
  return {
    persister,
    maxAge: OFFLINE_CACHE_MAX_AGE,
    buster,
    dehydrateOptions: { shouldDehydrateQuery: shouldPersistQuery },
  };
}

/**
 * Deletes the saved copy (sign-out, a 401): what is persisted next holds
 * the new identity's data only. A failure is logged, never thrown, so the
 * sign-out goes on.
 */
export async function clearOfflineCache(): Promise<void> {
  try {
    await active?.removeClient();
  } catch (err) {
    console.warn('[studio-schedule] could not delete the offline cache', err);
  }
}

/** Sets the persister clearOfflineCache deletes (for tests). */
export function setOfflinePersister(persister: Persister | null): void {
  active = persister;
}
