import { QueryClient } from '@tanstack/react-query';
import { createBrowserHistory, createRouter } from '@tanstack/react-router';
import { ApiError, sessionCaches } from '@/api/queries';
import { routerHistory } from '@/lib/env';
import { watchOnline } from '@/lib/online';
import { createAppHashHistory } from '@/lib/hashHistory';
import { routeTree } from './routeTree.gen';

// Offline (ADR 0017) queries pause and show the offline cache.
watchOnline();

export const queryClient: QueryClient = new QueryClient({
  // Any 401 signs out and clears the cache.
  ...sessionCaches(() => queryClient),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // Retry once on network and server errors; a client error such as a
      // 404 will not go away by asking again.
      retry: (failures, error) =>
        failures < 1 && !(error instanceof ApiError && error.status < 500),
      refetchOnWindowFocus: false,
    },
    mutations: {
      // A write runs at once, offline too, where the transport refuses it
      // (OfflineError) rather than sending it later unseen: the app is
      // read-only offline (ADR 0017).
      networkMode: 'always',
    },
  },
});

// Hash history for static hosting (GitHub Pages demo), browser history otherwise.
const history = routerHistory === 'hash' ? createAppHashHistory() : createBrowserHistory();
const basepath =
  routerHistory === 'hash' ? '/' : import.meta.env.BASE_URL.replace(/\/$/, '') || '/';

export const router = createRouter({
  routeTree,
  history,
  basepath,
  context: { queryClient },
  defaultPreload: 'intent',
  scrollRestoration: true,
});

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
