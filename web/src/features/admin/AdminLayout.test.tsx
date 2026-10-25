import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import {
  accounts,
  meAdmin,
  meImpersonated,
  meMember,
  meOffice,
  mockApiGet,
  mockApiPost,
  problem,
  signedOut,
} from '@/test/mockApi';
import { AccountsPage } from './AccountsPage';
import { AdminLayout } from './AdminLayout';

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * Renders the admin layout at `at` with stand-ins for its sections (or the
 * real accounts page) and a home page.
 */
function renderAdmin(at: string, { accounts }: { accounts?: () => ReactElement } = {}) {
  const rootRoute = createRootRoute({ component: Outlet });
  const homeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: () => <p>Home page</p>,
  });
  const adminRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/admin',
    component: AdminLayout,
  });
  const section = (path: string, text: string, component = () => <p>{text}</p>) =>
    createRoute({ getParentRoute: () => adminRoute, path, component });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      homeRoute,
      adminRoute.addChildren([
        section('/locations', 'Locations section'),
        section('/course-types', 'Course types section'),
        section('/pass-types', 'Passes section'),
        section('/staff', 'Staff section'),
        section('/terms', 'Terms section'),
        section('/persons', 'Persons section'),
        section('/receipts', 'Receipts section'),
        section('/statistics', 'Statistics section'),
        section('/announcements', 'Announcements section'),
        section('/accounts', 'Accounts section', accounts),
        section('/audit', 'Audit log section'),
        section('/mails', 'Mail log section'),
        section('/settings', 'Settings section'),
      ]),
    ]),
    history: createMemoryHistory({ initialEntries: [at] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { router, queryClient };
}

const sections = async () =>
  within(await screen.findByRole('navigation', { name: 'Administration' }));

describe('AdminLayout', () => {
  it.each([
    ['a member', { '/api/v1/auth/me': { body: meMember } }],
    ['a visitor who is not signed in', signedOut],
  ])('shows no access to %s', async (_, fixtures) => {
    mockApiGet(fixtures);

    renderAdmin('/admin/locations');

    expect(await screen.findByRole('heading', { level: 1, name: 'No access' })).toBeInTheDocument();
    expect(
      screen.getByText('Only the office and administrators can see this page.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument();
    expect(screen.queryByText('Locations section')).not.toBeInTheDocument();
  });

  it('shows the office the catalogue sections but not the accounts or the audit log', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meOffice } });

    renderAdmin('/admin/locations');

    const nav = await sections();
    expect(nav.getByRole('link', { name: 'Locations' })).toHaveAttribute(
      'href',
      '/admin/locations',
    );
    expect(nav.getByRole('link', { name: 'Locations' })).toHaveAttribute('aria-current', 'page');
    expect(nav.getByRole('link', { name: 'Course types' })).toHaveAttribute(
      'href',
      '/admin/course-types',
    );
    expect(nav.getByRole('link', { name: 'Staff' })).toHaveAttribute('href', '/admin/staff');
    expect(nav.getByRole('link', { name: 'Timetable' })).toHaveAttribute('href', '/admin/terms');
    expect(nav.getByRole('link', { name: 'Persons' })).toHaveAttribute('href', '/admin/persons');
    expect(nav.getByRole('link', { name: 'Passes' })).toHaveAttribute('href', '/admin/pass-types');
    expect(nav.getByRole('link', { name: 'Settings' })).toHaveAttribute('href', '/admin/settings');
    expect(nav.getByRole('link', { name: 'Statistics' })).toHaveAttribute(
      'href',
      '/admin/statistics',
    );
    expect(nav.queryByRole('link', { name: 'Accounts' })).not.toBeInTheDocument();
    expect(nav.queryByRole('link', { name: 'Audit log' })).not.toBeInTheDocument();
    // The office writes announcements; only admins see the mail log.
    expect(nav.getByRole('link', { name: 'Announcements' })).toHaveAttribute(
      'href',
      '/admin/announcements',
    );
    expect(nav.queryByRole('link', { name: 'Mail log' })).not.toBeInTheDocument();
    expect(screen.getByText('Locations section')).toBeInTheDocument();
  });

  it('opens the staff from the navigation', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meOffice } });
    const user = userEvent.setup();

    const { router } = renderAdmin('/admin/locations');
    await user.click((await sections()).getByRole('link', { name: 'Staff' }));

    expect(await screen.findByText('Staff section')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin/staff');
    expect((await sections()).getByRole('link', { name: 'Staff' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  it('shows the admin every section and switches between them', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meAdmin } });
    const user = userEvent.setup();

    const { router } = renderAdmin('/admin/locations');

    const nav = await sections();
    expect(nav.getAllByRole('link').map((a) => a.textContent)).toEqual([
      'Locations',
      'Course types',
      'Passes',
      'Staff',
      'Timetable',
      'Persons',
      'Receipts',
      'Statistics',
      'Announcements',
      'Accounts',
      'Audit log',
      'Mail log',
      'Settings',
    ]);
    await user.click(nav.getByRole('link', { name: 'Accounts' }));

    expect(await screen.findByText('Accounts section')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin/accounts');

    await user.click((await sections()).getByRole('link', { name: 'Audit log' }));

    expect(await screen.findByText('Audit log section')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/admin/audit');
  });

  it('says when it cannot check the access, and checks again', async () => {
    let fail = true;
    mockApiGet({
      '/api/v1/auth/me': () => (fail ? problem(503, 'unavailable') : { body: meOffice }),
    });
    const user = userEvent.setup();

    renderAdmin('/admin/locations');

    expect(
      await screen.findByText('Could not check whether you may see this page.'),
    ).toBeInTheDocument();
    expect(screen.queryByText('No access')).not.toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByText('Locations section')).toBeInTheDocument();
  });

  it('goes home after signing in as a member, although the layout drops the accounts page', async () => {
    mockApiGet({
      '/api/v1/auth/me': { body: meAdmin },
      '/api/v1/admin/accounts': { body: { items: accounts } },
    });
    mockApiPost({
      '/api/v1/admin/accounts/{accountId}/impersonate': { body: meImpersonated },
    });
    const user = userEvent.setup();

    const { router, queryClient } = renderAdmin('/admin/accounts', { accounts: AccountsPage });
    await user.click(await screen.findByRole('button', { name: 'Sign in as Nora Example' }));

    await vi.waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(await screen.findByText('Home page')).toBeInTheDocument();
    expect(queryClient.getQueryData(['auth', 'me'])).toEqual(meImpersonated);
  });

  it('renders in German', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meAdmin } });
    await i18n.changeLanguage('de');

    renderAdmin('/admin/locations');

    const nav = within(await screen.findByRole('navigation', { name: 'Verwaltung' }));
    expect(nav.getByRole('link', { name: 'Standorte' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Kursarten' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Personal' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Stundenplan' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Personen' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Konten' })).toBeInTheDocument();
    expect(nav.getByRole('link', { name: 'Protokoll' })).toBeInTheDocument();
  });
});
