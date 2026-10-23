import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import {
  accounts,
  meAdmin,
  meImpersonated,
  meMember,
  mockApiGet,
  mockApiPost,
  problem,
  signedOut,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { AccountsPage } from './AccountsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asAdmin = {
  '/api/v1/auth/me': { body: meAdmin },
  '/api/v1/admin/accounts': { body: { items: accounts } },
};

function renderPage() {
  return renderInRouter(<AccountsPage />, { at: '/admin/accounts' });
}

/** The table row of the account with the given email address. */
async function row(email: string) {
  const cell = await screen.findByRole('cell', { name: email });
  const tr = cell.closest('tr');
  if (!tr) throw new Error(`no row for ${email}`);
  return within(tr);
}

describe('AccountsPage', () => {
  it('lists every account with name, email, role and status', async () => {
    mockApiGet(asAdmin);

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Accounts' })).toBeInTheDocument();
    const table = within(await screen.findByRole('table', { name: 'Accounts' }));
    expect(table.getAllByRole('columnheader').map((th) => th.textContent)).toEqual([
      'Name',
      'Email',
      'Role',
      'Status',
      'Actions',
    ]);
    // A header row and one row per account, in the API's order.
    expect(table.getAllByRole('row')).toHaveLength(accounts.length + 1);

    const nora = await row('nora.example@example.org');
    expect(nora.getByText('Nora Example')).toBeInTheDocument();
    expect(nora.getByText('Member')).toBeInTheDocument();
    expect(nora.getByText('Active')).toBeInTheDocument();
    expect((await row('alex.example@example.com')).getByText('Instructor')).toBeInTheDocument();
    expect((await row('nina.new@example.org')).getByText('Unconfirmed')).toBeInTheDocument();
    expect((await row('olaf.old@example.org')).getByText('Disabled')).toBeInTheDocument();
  });

  it('offers "Sign in as" only for confirmed, enabled accounts that are no admins', async () => {
    mockApiGet(asAdmin);

    renderPage();

    const signInAs = (name: string) => screen.findByRole('button', { name: `Sign in as ${name}` });
    expect(await signInAs('Nora Example')).toBeEnabled();
    expect(await signInAs('Alex Example')).toBeEnabled();
    expect(await signInAs('Ada Admin')).toBeDisabled();
    expect(await signInAs('Nina New')).toBeDisabled();
    expect(await signInAs('Olaf Old')).toBeDisabled();
  });

  it('signs in as the account and goes to the home page', async () => {
    mockApiGet(asAdmin);
    const post = mockApiPost({
      '/api/v1/admin/accounts/{accountId}/impersonate': { body: meImpersonated },
    });
    const user = userEvent.setup();

    const { router, queryClient } = renderPage();
    await user.click(await screen.findByRole('button', { name: 'Sign in as Nora Example' }));

    await vi.waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(post).toHaveBeenCalledWith('/api/v1/admin/accounts/{accountId}/impersonate', {
      params: { path: { accountId: meMember.accountId } },
    });
    expect(queryClient.getQueryData(['auth', 'me'])).toEqual(meImpersonated);
  });

  it('says so when signing in as the account fails', async () => {
    mockApiGet(asAdmin);
    mockApiPost({
      '/api/v1/admin/accounts/{accountId}/impersonate': problem(409, 'account disabled'),
    });
    const user = userEvent.setup();

    const { router } = renderPage();
    await user.click(await screen.findByRole('button', { name: 'Sign in as Nora Example' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Could not sign in as Nora Example.',
    );
    expect(router.state.location.pathname).toBe('/admin/accounts');
  });

  it.each([
    ['a member', { '/api/v1/auth/me': { body: meMember } }],
    ['a visitor who is not signed in', signedOut],
  ])('shows no access to %s and does not ask for the accounts', async (_, fixtures) => {
    const get = mockApiGet(fixtures);

    renderPage();

    expect(await screen.findByText('Only administrators can see this page.')).toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalledWith('/api/v1/admin/accounts', expect.anything());
  });

  it('shows an error with retry when the accounts cannot be loaded', async () => {
    let fail = true;
    mockApiGet({
      '/api/v1/auth/me': { body: meAdmin },
      '/api/v1/admin/accounts': () => (fail ? problem(500, 'boom') : { body: { items: accounts } }),
    });
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByText('Could not load the accounts.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('table', { name: 'Accounts' })).toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockApiGet(asAdmin);
    await i18n.changeLanguage('de');

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Konten' })).toBeInTheDocument();
    const nora = await row('nora.example@example.org');
    expect(nora.getByText('Mitglied')).toBeInTheDocument();
    expect(nora.getByText('Aktiv')).toBeInTheDocument();
    expect(nora.getByRole('button', { name: 'Als Nora Example anmelden' })).toBeEnabled();
  });
});
