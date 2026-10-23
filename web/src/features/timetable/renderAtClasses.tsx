import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { render } from '@testing-library/react';
import type { ReactElement } from 'react';
import { type TimetableSearch, validateTimetableSearch } from './search';

/**
 * Test helper: renders ui as the /classes page with the given search params,
 * in a router that also knows '/' and '/schedule' (rendering nothing), so
 * router hooks and links work. Read the URL from `router.state.location`.
 * The route renders asynchronously: use findBy* for the first assertion.
 */
export function renderAtClasses(ui: ReactElement, search: TimetableSearch = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
  const rootRoute = createRootRoute({ component: Outlet });
  const empty = (path: string) =>
    createRoute({ getParentRoute: () => rootRoute, path, component: () => null });
  const classesRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/classes',
    validateSearch: validateTimetableSearch,
    component: () => ui,
  });
  const query = search.term ? `?term=${search.term}` : '';
  const router = createRouter({
    routeTree: rootRoute.addChildren([empty('/'), empty('/schedule'), classesRoute]),
    history: createMemoryHistory({ initialEntries: [`/classes${query}`] }),
  });
  return {
    queryClient,
    router,
    ...render(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    ),
  };
}
