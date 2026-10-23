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
import { type ScheduleSearch, validateScheduleSearch } from '@/features/schedule/search';

function testQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  });
}

export function renderWithQuery(ui: ReactElement) {
  const queryClient = testQueryClient();
  return {
    queryClient,
    ...render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>),
  };
}

/**
 * Renders ui as the page of a /schedule route with the given search params,
 * so components can use useScheduleSearch. Read the current params from
 * `router.state.location.search`. The route renders asynchronously: use
 * findBy* queries for the first assertion.
 */
export function renderAtSchedule(ui: ReactElement, search: ScheduleSearch = {}) {
  const queryClient = testQueryClient();
  const rootRoute = createRootRoute({ component: Outlet });
  const scheduleRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/schedule',
    validateSearch: validateScheduleSearch,
    component: () => ui,
  });
  const query = new URLSearchParams(
    Object.entries(search).filter((e): e is [string, string] => e[1] !== undefined),
  ).toString();
  const router = createRouter({
    routeTree: rootRoute.addChildren([scheduleRoute]),
    history: createMemoryHistory({ initialEntries: [`/schedule${query ? `?${query}` : ''}`] }),
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
