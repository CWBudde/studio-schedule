import { useQuery } from '@tanstack/react-query';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'sonner';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { familyQuery } from '@/api/queries';
import {
  family,
  meAdmin,
  meMember,
  meOffice,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  signedOut,
} from '@/test/mockApi';
import { Toaster } from '@/components/ui/sonner';
import { renderInRouter } from '@/test/utils';
import { Header } from '@/themes/programme/Shell';
import { useShellHeader } from './AppHeader';

/** Today's header: the Shell's header as Programme shows it. */
function AppHeader() {
  return <Header {...useShellHeader()} />;
}

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

  it('links staff to the instructor view', async () => {
    for (const me of [{ ...meMember, role: 'instructor' as const }, meOffice, meAdmin]) {
      mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: me } });
      const rendered = renderInRouter(<AppHeader />);

      const nav = within(await screen.findByRole('navigation', { name: 'Main' }));
      expect(await nav.findByRole('link', { name: 'Teaching' })).toHaveAttribute('href', '/teach');
      rendered.unmount();
      vi.restoreAllMocks();
    }
  });

  it('links signed-in accounts to their lessons', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meMember } });

    renderInRouter(<AppHeader />);

    const nav = within(await screen.findByRole('navigation', { name: 'Main' }));
    expect(await nav.findByRole('link', { name: 'My lessons' })).toHaveAttribute(
      'href',
      '/my-lessons',
    );
  });

  it('has no link to my lessons for visitors', async () => {
    mockApiGet({ ...signedOut, ...notDemo });

    renderInRouter(<AppHeader />);

    expect(await screen.findByRole('link', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'My lessons' })).not.toBeInTheDocument();
  });

  it.each([
    ['members', { '/api/v1/auth/me': { body: meMember } }, 'button', 'Nora Example'],
    ['visitors', signedOut, 'link', 'Sign in'],
  ] as const)('has no instructor view for %s', async (_, fixtures, role, name) => {
    mockApiGet({ ...notDemo, ...fixtures });

    renderInRouter(<AppHeader />);

    // The account menu shows once `me` has answered.
    expect(await screen.findByRole(role, { name })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Teaching' })).not.toBeInTheDocument();
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

  it('signs out and forgets the data of the account when any read answers 401', async () => {
    // The session ends while the page is open (it expired, or ended on
    // another device): the next read of private data is a 401.
    let sessionOver = false;
    mockApiGet({
      ...notDemo,
      '/api/v1/auth/me': () => (sessionOver ? { status: 401 } : { body: meMember }),
      '/api/v1/family': () => (sessionOver ? problem(401, 'not signed in') : { body: family }),
    });
    function FamilyName() {
      const { data } = useQuery(familyQuery);
      return <p>{data ? `Family of ${data.me.givenName}` : 'No family data'}</p>;
    }

    const { queryClient } = renderInRouter(
      <>
        <AppHeader />
        <FamilyName />
      </>,
    );
    expect(await screen.findByRole('button', { name: 'Nora Example' })).toBeInTheDocument();
    expect(await screen.findByText('Family of Nora')).toBeInTheDocument();

    sessionOver = true;
    await queryClient.refetchQueries({ queryKey: familyQuery.queryKey });

    expect(await screen.findByRole('link', { name: 'Sign in' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Nora Example' })).not.toBeInTheDocument();
    expect(await screen.findByText('No family data')).toBeInTheDocument();
    expect(queryClient.getQueryData(familyQuery.queryKey)).toBeUndefined();
  });

  describe('the session, for a theme with a sign-in/out button of its own', () => {
    // Sonner's store outlives a test: the account menu's sign-out above leaves its notice.
    beforeEach(() => {
      toast.dismiss();
    });

    /** The header hook's session, as a theme's Shell gets it. */
    function Session() {
      const { session } = useShellHeader();
      if (session.state !== 'signedIn') return <p>Session: {session.state}</p>;
      return (
        <button type="button" disabled={session.signingOut} onClick={session.signOut}>
          Theme sign-out
        </button>
      );
    }

    it('is signed out for a visitor', async () => {
      mockApiGet({ ...signedOut, ...notDemo });

      renderInRouter(<Session />);

      expect(await screen.findByText('Session: signedOut')).toBeInTheDocument();
    });

    it("signs out like the account menu, with the menu's notice", async () => {
      mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meMember } });
      const post = mockApiPost({ '/api/v1/auth/logout': { status: 204 } });
      const user = userEvent.setup();

      renderInRouter(
        <>
          <Session />
          <Toaster />
        </>,
      );
      await user.click(await screen.findByRole('button', { name: 'Theme sign-out' }));

      expect(await screen.findByText('Session: signedOut')).toBeInTheDocument();
      expect(post).toHaveBeenCalledWith('/api/v1/auth/logout');
      expect(await screen.findByText('You have been signed out.')).toBeInTheDocument();
    });
  });
});
