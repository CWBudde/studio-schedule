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

/** The routes of the test router; each renders ui when it is the page `at`. */
export type TestPath = '/' | '/schedule' | '/login' | '/register' | '/confirm';

/**
 * Renders ui as the page at `at` (default '/') of a router with the app's
 * routes, so components can use router hooks and links. The other routes
 * render nothing. Read the current URL from `router.state.location`. The
 * route renders asynchronously: use findBy* queries for the first assertion.
 */
export function renderInRouter(
  ui: ReactElement,
  { at = '/', search = {} }: { at?: TestPath; search?: Record<string, string | undefined> } = {},
) {
  const queryClient = testQueryClient();
  const rootRoute = createRootRoute({ component: Outlet });
  const page = (path: TestPath) => () => (at === path ? ui : null);
  const route = (path: Exclude<TestPath, '/schedule'>) =>
    createRoute({ getParentRoute: () => rootRoute, path, component: page(path) });
  const scheduleRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/schedule',
    validateSearch: validateScheduleSearch,
    component: page('/schedule'),
  });
  const query = new URLSearchParams(
    Object.entries(search).filter((e): e is [string, string] => e[1] !== undefined),
  ).toString();
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      route('/'),
      scheduleRoute,
      route('/login'),
      route('/register'),
      route('/confirm'),
    ]),
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
