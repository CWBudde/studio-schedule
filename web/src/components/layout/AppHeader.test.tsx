import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  meAdmin,
  meMember,
  meOffice,
  mockApiGet,
  mockApiPost,
  publicSettings,
  signedOut,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { AppHeader } from './AppHeader';

afterEach(() => {
  vi.restoreAllMocks();
});

const notDemo = { '/api/v1/demo/mailbox': { status: 404, body: undefined } };

describe('AppHeader', () => {
  it('links to the sign-in page when signed out', async () => {
    mockApiGet({ ...signedOut, ...notDemo });

    renderInRouter(<AppHeader />);

    expect(await screen.findByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(screen.getByRole('button', { name: 'Change language' })).toBeInTheDocument();
  });

  it('links to the schedule and the classes', async () => {
    mockApiGet({ ...signedOut, ...notDemo });

    renderInRouter(<AppHeader />);

    const nav = within(await screen.findByRole('navigation', { name: 'Main' }));
    expect(nav.getByRole('link', { name: 'Schedule' })).toHaveAttribute('href', '/schedule');
    expect(nav.getByRole('link', { name: 'Classes' })).toHaveAttribute('href', '/classes');
  });

  it('shows the signed-in person and signs out from the account menu', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meMember } });
    const post = mockApiPost({ '/api/v1/auth/logout': { status: 204 } });
    const user = userEvent.setup();

    renderInRouter(<AppHeader />);

    await user.click(await screen.findByRole('button', { name: 'Nora Example' }));
    expect(await screen.findByText('nora.example@example.org')).toBeInTheDocument();
    await user.click(screen.getByRole('menuitem', { name: 'Sign out' }));

    expect(await screen.findByRole('link', { name: 'Sign in' })).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/auth/logout');
    expect(screen.queryByRole('button', { name: 'Nora Example' })).not.toBeInTheDocument();
  });

  it('links admins to the accounts from the account menu', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meAdmin } });
    const user = userEvent.setup();

    const { router } = renderInRouter(<AppHeader />);

    await user.click(await screen.findByRole('button', { name: 'Ada Admin' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Accounts' }));

    await vi.waitFor(() => expect(router.state.location.pathname).toBe('/admin/accounts'));
  });

  it('links to the account settings from the account menu', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meMember } });
    const user = userEvent.setup();

    const { router } = renderInRouter(<AppHeader />);

    await user.click(await screen.findByRole('button', { name: 'Nora Example' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Account settings' }));

    await vi.waitFor(() => expect(router.state.location.pathname).toBe('/account'));
  });

  it('links the office to the administration but not to the accounts', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meOffice } });
    const user = userEvent.setup();

    renderInRouter(<AppHeader />);

    await user.click(await screen.findByRole('button', { name: 'Otto Office' }));
    expect(await screen.findByRole('menuitem', { name: 'Administration' })).toHaveAttribute(
      'href',
      '/admin',
    );
    expect(screen.queryByRole('menuitem', { name: 'Accounts' })).not.toBeInTheDocument();
  });

  it('links admins to the administration as well', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meAdmin } });
    const user = userEvent.setup();

    renderInRouter(<AppHeader />);

    await user.click(await screen.findByRole('button', { name: 'Ada Admin' }));
    expect(await screen.findByRole('menuitem', { name: 'Administration' })).toHaveAttribute(
      'href',
      '/admin',
    );
  });

  it('has no administration or accounts link for other roles', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meMember } });
    const user = userEvent.setup();

    renderInRouter(<AppHeader />);

    await user.click(await screen.findByRole('button', { name: 'Nora Example' }));
    expect(await screen.findByRole('menuitem', { name: 'Sign out' })).toBeInTheDocument();
    expect(screen.queryByRole('menuitem', { name: 'Administration' })).not.toBeInTheDocument();
    expect(screen.queryByRole('menuitem', { name: 'Accounts' })).not.toBeInTheDocument();
  });

  it('has a demo mailbox button only in a demo', async () => {
    const get = mockApiGet({ ...signedOut, ...notDemo });

    const first = renderInRouter(<AppHeader />);
    expect(await screen.findByRole('link', { name: 'Sign in' })).toBeInTheDocument();
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith('/api/v1/demo/mailbox', expect.anything()),
    );
    expect(screen.queryByRole('button', { name: /Demo mailbox/ })).not.toBeInTheDocument();
    first.unmount();
    vi.restoreAllMocks();

    mockApiGet({
      ...signedOut,
      '/api/v1/demo/mailbox': { body: { mails: [] } },
      '/api/v1/settings/public': { body: publicSettings },
    });
    renderInRouter(<AppHeader />);
    expect(await screen.findByRole('button', { name: /Demo mailbox/ })).toBeInTheDocument();
    // A demo server over HTTP keeps nothing in the browser, so there is
    // nothing to reset (ResetDemo.test.tsx covers the web demo).
    expect(screen.queryByRole('button', { name: 'Reset demo' })).not.toBeInTheDocument();
  });
});
