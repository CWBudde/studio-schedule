import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { meMember, mockApiGet, mockApiPost, publicSettings, signedOut } from '@/test/mockApi';
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
  });
});
