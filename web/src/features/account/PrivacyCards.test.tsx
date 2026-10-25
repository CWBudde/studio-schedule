import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  conflict,
  family,
  meAdmin,
  meImpersonated,
  meMember,
  mockApiDelete,
  mockApiGet,
  type MockRequest,
  personExport,
  problem,
} from '@/test/mockApi';
import { captureDownloads } from '@/test/downloads';
import { renderInRouter } from '@/test/utils';
import { AccountPage } from './AccountPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const signedInMember = {
  '/api/v1/demo/mailbox': { status: 404, body: undefined },
  '/api/v1/auth/me': { body: meMember },
  '/api/v1/family': { body: family },
  '/api/v1/my/calendar-feeds': { body: { items: [] } },
};

function renderAccount() {
  return renderInRouter(<AccountPage />, { at: '/account' });
}

const dataCard = async () => within(await screen.findByRole('region', { name: 'Your data' }));
const deleteCard = async () =>
  within(await screen.findByRole('region', { name: 'Delete account' }));

describe('MyDataCard', () => {
  it('downloads the data of the person and of each child as a JSON file', async () => {
    const asked: unknown[] = [];
    mockApiGet({
      ...signedInMember,
      '/api/v1/my/export': (init: MockRequest) => {
        asked.push(init.params?.query?.person);
        const lina = family.dependents[0]!;
        return {
          body:
            init.params?.query?.person === lina.id
              ? personExport({ id: lina.id, givenName: 'Lina', familyName: 'Example' })
              : personExport(),
        };
      },
    });
    const saved = captureDownloads();
    const user = userEvent.setup();

    renderAccount();
    const card = await dataCard();
    expect(await card.findByText('Nora Example (you)')).toBeInTheDocument();
    expect(card.getByText('Max Example')).toBeInTheDocument();

    await user.click(card.getByRole('button', { name: 'Download the data of Nora Example' }));
    await vi.waitFor(() => expect(saved).toHaveLength(1));
    expect(saved[0]?.name).toBe('data-export-nora-example-2026-10-25.json');
    const json = JSON.parse((await saved[0]?.blob.text()) ?? '') as { person: { email: string } };
    expect(json.person.email).toBe('nora@example.org');

    await user.click(card.getByRole('button', { name: 'Download the data of Lina Example' }));
    await vi.waitFor(() => expect(saved).toHaveLength(2));
    expect(saved[1]?.name).toBe('data-export-lina-example-2026-10-25.json');
    expect(asked).toEqual([meMember.personId, family.dependents[0]?.id]);
  });

  it('says so when the child left the family meanwhile', async () => {
    mockApiGet({ ...signedInMember, '/api/v1/my/export': problem(404, 'not found') });
    captureDownloads();
    const user = userEvent.setup();

    renderAccount();
    const card = await dataCard();
    await user.click(await card.findByRole('button', { name: 'Download the data of Max Example' }));

    expect(
      await card.findByText('This person is no longer part of your family.'),
    ).toBeInTheDocument();
  });
});

describe('DeleteAccountCard', () => {
  async function deleteAccount(password: string) {
    const user = userEvent.setup();
    const card = await deleteCard();
    await user.type(card.getByLabelText('Current password'), password);
    await user.click(card.getByRole('button', { name: 'Delete my account' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete your account?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete account' }));
    return card;
  }

  it('deletes the account after the password and a confirmation, and signs out', async () => {
    mockApiGet(signedInMember);
    const del = mockApiDelete({ '/api/v1/my/account': { status: 204 } });

    const { router, queryClient } = renderAccount();
    await deleteAccount('demo-password');

    await vi.waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(del).toHaveBeenCalledWith('/api/v1/my/account', {
      body: { password: 'demo-password', locale: 'en' },
    });
    expect(queryClient.getQueryData(['auth', 'me'])).toBeNull();
  });

  it('marks a wrong password', async () => {
    mockApiGet(signedInMember);
    mockApiDelete({
      '/api/v1/my/account': problem(422, 'validation failed', [
        { location: 'body.password', message: 'the password is wrong' },
      ]),
    });

    renderAccount();
    const card = await deleteAccount('wrong');

    expect(await card.findByText('The password is incorrect.')).toBeInTheDocument();
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('explains why the last admin cannot leave', async () => {
    mockApiGet({ ...signedInMember, '/api/v1/auth/me': { body: meAdmin } });
    mockApiDelete({ '/api/v1/my/account': conflict('last-admin') });

    renderAccount();
    const card = await deleteAccount('demo-password');

    expect(
      await card.findByText(
        'You are the school’s only administrator. Make someone else an administrator first.',
      ),
    ).toBeInTheDocument();
  });

  it('is not offered while an admin is signed in as the account', async () => {
    mockApiGet({ ...signedInMember, '/api/v1/auth/me': { body: meImpersonated } });

    renderAccount();

    expect(await screen.findByRole('region', { name: 'Your data' })).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Delete account' })).not.toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockApiGet(signedInMember);
    const { default: i18n } = await import('@/i18n');
    await i18n.changeLanguage('de');

    renderAccount();

    expect(await screen.findByRole('region', { name: 'Konto löschen' })).toBeInTheDocument();
    expect(await screen.findByRole('region', { name: 'Datenexport' })).toBeInTheDocument();
  });
});
