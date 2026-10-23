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
 * Renders ui as the page at `at` ('/' or '/schedule') of a router with both
 * routes, so components can use router hooks and links. The other route
 * renders nothing. Read the current URL from `router.state.location`. The
 * route renders asynchronously: use findBy* queries for the first assertion.
 */
export function renderInRouter(
  ui: ReactElement,
  { at = '/', search = {} }: { at?: '/' | '/schedule'; search?: ScheduleSearch } = {},
) {
  const queryClient = testQueryClient();
  const rootRoute = createRootRoute({ component: Outlet });
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: () => (at === '/' ? ui : null),
  });
  const scheduleRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/schedule',
    validateSearch: validateScheduleSearch,
    component: () => (at === '/schedule' ? ui : null),
  });
  const query = new URLSearchParams(
    Object.entries(search).filter((e): e is [string, string] => e[1] !== undefined),
  ).toString();
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute, scheduleRoute]),
    history: createMemoryHistory({ initialEntries: [`${at}${query ? `?${query}` : ''}`] }),
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

/** Renders ui as the /schedule page with the given search params (see renderInRouter). */
export function renderAtSchedule(ui: ReactElement, search: ScheduleSearch = {}) {
  return renderInRouter(ui, { at: '/schedule', search });
}
