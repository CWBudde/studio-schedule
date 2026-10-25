import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AuditEventEntry } from '@/api/client';
import { Toaster } from '@/components/ui/sonner';
import {
  accounts,
  conflict,
  meAdmin,
  meMember,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  publicSettings,
} from '@/test/mockApi';
import { renderInRouter, renderWithQuery } from '@/test/utils';
import { AccountsPage } from './AccountsPage';
import { AuditPage } from './AuditPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asAdmin = {
  '/api/v1/auth/me': { body: meAdmin },
  '/api/v1/admin/accounts': { body: { items: accounts } },
};

function renderPage() {
  render(<Toaster />);
  return renderInRouter(<AccountsPage />, { at: '/admin/accounts' });
}

const nora = accounts.find((a) => a.id === meMember.accountId)!;

async function openMenu(name: string) {
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: `More actions for ${name}` }));
  return user;
}

describe('AccountActions', () => {
  it('disables an account after asking', async () => {
    mockApiGet(asAdmin);
    const post = mockApiPost({
      '/api/v1/admin/accounts/{accountId}/disable': { body: { ...nora, disabled: true } },
    });

    renderPage();
    const user = await openMenu('Nora Example');
    await user.click(await screen.findByRole('menuitem', { name: 'Disable' }));
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Disable the account of Nora Example?' }),
    );
    await user.click(dialog.getByRole('button', { name: 'Disable' }));

    expect(await screen.findByText('Nora Example can no longer sign in.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/accounts/{accountId}/disable', {
      params: { path: { accountId: nora.id } },
    });
  });

  it('enables a disabled account at once', async () => {
    mockApiGet(asAdmin);
    const post = mockApiPost({
      '/api/v1/admin/accounts/{accountId}/enable': { body: { ...accounts[4], disabled: false } },
    });

    renderPage();
    const user = await openMenu('Olaf Old');
    await user.click(await screen.findByRole('menuitem', { name: 'Enable' }));

    expect(await screen.findByText('Olaf Old can sign in again.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/accounts/{accountId}/enable', {
      params: { path: { accountId: accounts[4]?.id } },
    });
  });

  it('changes the role', async () => {
    mockApiGet(asAdmin);
    const put = mockApiPut({
      '/api/v1/admin/accounts/{accountId}/role': { body: { ...nora, role: 'instructor' } },
    });

    renderPage();
    const user = await openMenu('Nora Example');
    await user.click(await screen.findByRole('menuitem', { name: 'Change role' }));
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Change the role of Nora Example' }),
    );
    await user.click(dialog.getByRole('combobox', { name: 'Role' }));
    await user.click(await screen.findByRole('option', { name: 'Instructor' }));
    await user.click(dialog.getByRole('button', { name: 'Save role' }));

    expect(await screen.findByText('Nora Example is now: Instructor.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/accounts/{accountId}/role', {
      params: { path: { accountId: nora.id } },
      body: { role: 'instructor' },
    });
  });

  it('deletes an account, and explains one that is kept', async () => {
    mockApiGet(asAdmin);
    let kept = true;
    mockApiDelete({
      '/api/v1/admin/accounts/{accountId}': () => (kept ? conflict('in-use') : { status: 204 }),
    });

    renderPage();
    const user = await openMenu('Nora Example');
    await user.click(await screen.findByRole('menuitem', { name: 'Delete' }));
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Delete the account of Nora Example?' }),
    );
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    expect(
      await dialog.findByText(/name the account of Nora Example, so it cannot be deleted/),
    ).toBeInTheDocument();

    kept = false;
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    expect(
      await screen.findByText('The account of Nora Example has been deleted.'),
    ).toBeInTheDocument();
  });

  it('explains the last admin', async () => {
    mockApiGet(asAdmin);
    mockApiPost({ '/api/v1/admin/accounts/{accountId}/disable': conflict('last-admin') });

    renderPage();
    const user = await openMenu('Alex Example');
    await user.click(await screen.findByRole('menuitem', { name: 'Disable' }));
    const dialog = within(await screen.findByRole('alertdialog'));
    await user.click(dialog.getByRole('button', { name: 'Disable' }));

    expect(
      await dialog.findByText(/This is the school’s last administrator account that can sign in/),
    ).toBeInTheDocument();
  });

  it('offers no actions on the admin’s own account', async () => {
    mockApiGet(asAdmin);

    renderPage();

    expect(
      await screen.findByRole('button', { name: 'More actions for Ada Admin' }),
    ).toBeDisabled();
    expect(screen.getByRole('button', { name: 'More actions for Nora Example' })).toBeEnabled();
  });
});

describe('AuditPage, account administration', () => {
  it('names the new actions, a role change and an account its holder deleted', async () => {
    const admin = {
      id: meAdmin.accountId,
      email: meAdmin.email,
      givenName: 'Ada',
      familyName: 'Admin',
    };
    const events: AuditEventEntry[] = [
      {
        id: '0190a000-0000-7000-8000-000000000391',
        at: '2026-10-25T12:00:00Z',
        action: 'account-deleted',
        targetPerson: { id: meMember.personId, givenName: 'Nora', familyName: 'Example' },
      },
      {
        id: '0190a000-0000-7000-8000-000000000392',
        at: '2026-10-25T11:00:00Z',
        action: 'account-role-changed',
        actor: admin,
        target: { id: nora.id, email: nora.email, givenName: 'Nora', familyName: 'Example' },
        details: { previousRole: 'member', role: 'office' },
      },
      {
        id: '0190a000-0000-7000-8000-000000000393',
        at: '2026-10-25T10:00:00Z',
        action: 'person-anonymized',
        actor: admin,
        targetPerson: { id: meMember.personId, givenName: 'Deleted', familyName: 'person' },
      },
    ];
    mockApiGet({
      '/api/v1/auth/me': { body: meAdmin },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/admin/audit': { body: { items: events } },
    });

    renderWithQuery(<AuditPage />);

    const table = within(await screen.findByRole('table', { name: 'Audit log' }));
    const rows = (await table.findAllByRole('row')).slice(1).map((tr) =>
      within(tr)
        .getAllByRole('cell')
        .map((td) => td.textContent),
    );
    expect(rows.map((r) => r[1])).toEqual(['Account deleted', 'Role changed', 'Person anonymized']);
    expect(rows[0]?.[2]).toBe('The account holder');
    expect(rows[1]?.[3]).toContain('Member → Office');
  });
});
