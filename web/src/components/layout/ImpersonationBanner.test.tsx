import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import {
  meAdmin,
  meImpersonated,
  meMember,
  mockApiGet,
  mockApiPost,
  problem,
  signedOut,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ImpersonationBanner } from './ImpersonationBanner';

afterEach(() => {
  vi.restoreAllMocks();
});

const banner = () => screen.findByRole('region', { name: 'Signed in as someone else' });

describe('ImpersonationBanner', () => {
  it('names the account and the admin while an admin is signed in as someone else', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meImpersonated } });

    renderInRouter(<ImpersonationBanner />);

    const region = within(await banner());
    expect(
      region.getByText(
        'You are signed in as Nora Example (nora.example@example.org) by Ada Admin.',
      ),
    ).toBeInTheDocument();
    expect(region.getByRole('button', { name: 'Return to my account' })).toBeInTheDocument();
  });

  it.each([
    ['signed out', signedOut],
    ['signed in normally', { '/api/v1/auth/me': { body: meMember } }],
  ])('shows nothing when %s', async (_, fixtures) => {
    const get = mockApiGet(fixtures);

    renderInRouter(<ImpersonationBanner />);

    await vi.waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/auth/me', expect.anything()));
    expect(screen.queryByRole('region', { name: 'Signed in as someone else' })).toBeNull();
  });

  it('returns to the admin account', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meImpersonated } });
    const post = mockApiPost({ '/api/v1/auth/impersonation/stop': { body: meAdmin } });
    const user = userEvent.setup();

    const { queryClient } = renderInRouter(<ImpersonationBanner />);
    await user.click(within(await banner()).getByRole('button', { name: 'Return to my account' }));

    await vi.waitFor(() =>
      expect(screen.queryByRole('region', { name: 'Signed in as someone else' })).toBeNull(),
    );
    expect(post).toHaveBeenCalledWith('/api/v1/auth/impersonation/stop');
    expect(queryClient.getQueryData(['auth', 'me'])).toEqual(meAdmin);
  });

  it('says so when returning fails', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meImpersonated } });
    mockApiPost({ '/api/v1/auth/impersonation/stop': problem(500, 'boom') });
    const user = userEvent.setup();

    renderInRouter(<ImpersonationBanner />);
    const region = within(await banner());
    await user.click(region.getByRole('button', { name: 'Return to my account' }));

    expect(
      await region.findByText('Could not return to your account. Please try again.'),
    ).toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meImpersonated } });
    await i18n.changeLanguage('de');
    renderInRouter(<ImpersonationBanner />);

    const region = within(
      await screen.findByRole('region', { name: 'Als andere Person angemeldet' }),
    );
    expect(
      region.getByText(
        'Du bist als Nora Example (nora.example@example.org) angemeldet, über das Konto von Ada Admin.',
      ),
    ).toBeInTheDocument();
    expect(region.getByRole('button', { name: 'Zurück zu meinem Konto' })).toBeInTheDocument();
  });
});
