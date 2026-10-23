import { QueryClient } from '@tanstack/react-query';
import { createBrowserHistory, createHashHistory, createRouter } from '@tanstack/react-router';
import { ApiError } from '@/api/queries';
import { routerHistory } from '@/lib/env';
import { routeTree } from './routeTree.gen';

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      // Retry once on network and server errors; a client error such as a
      // 404 will not go away by asking again.
      retry: (failures, error) =>
        failures < 1 && !(error instanceof ApiError && error.status < 500),
      refetchOnWindowFocus: false,
    },
  },
});

// Hash history for static hosting (GitHub Pages demo), browser history otherwise.
const history = routerHistory === 'hash' ? createHashHistory() : createBrowserHistory();
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
