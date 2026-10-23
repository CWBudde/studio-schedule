import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { meQuery } from '@/api/queries';
import { meMember, mockApiGet, mockApiPost, problem, signedOut } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ConfirmEmailChangePage } from './ConfirmEmailChangePage';

afterEach(() => {
  vi.restoreAllMocks();
});

function renderConfirm(token = 'tok-email') {
  return renderInRouter(<ConfirmEmailChangePage />, {
    at: '/confirm-email-change',
    search: { token },
  });
}

async function confirm() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Confirm new address' }));
}

describe('ConfirmEmailChangePage', () => {
  it('confirms the new address when signed out and links to the sign-in', async () => {
    mockApiGet(signedOut);
    const post = mockApiPost({ '/api/v1/auth/email/confirm': { status: 204 } });

    renderConfirm();
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Confirm your new email address' }),
    ).toBeInTheDocument();
    await confirm();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Your email address has been changed' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(post).toHaveBeenCalledWith('/api/v1/auth/email/confirm', {
      body: { token: 'tok-email', locale: 'en' },
    });
  });

  it('shows the new address of the signed-in account', async () => {
    const moved = { ...meMember, email: 'nora.new@example.org' };
    let current = meMember;
    mockApiGet({ '/api/v1/auth/me': () => ({ body: current }) });
    mockApiPost({
      '/api/v1/auth/email/confirm': () => {
        current = moved;
        return { status: 204 };
      },
    });

    const { queryClient } = renderConfirm();
    await vi.waitFor(() => expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meMember));
    await confirm();

    expect(
      await screen.findByText('Your account now uses nora.new@example.org.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to your account' })).toHaveAttribute(
      'href',
      '/account',
    );
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(moved);
  });

  it('does not claim the address of another signed-in account', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meMember } });
    mockApiPost({ '/api/v1/auth/email/confirm': { status: 204 } });

    const { queryClient } = renderConfirm();
    await vi.waitFor(() => expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meMember));
    await confirm();

    expect(
      await screen.findByText('From now on, sign in with your new address.'),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Your account now uses/)).not.toBeInTheDocument();
  });

  it('explains an invalid or expired link', async () => {
    mockApiGet(signedOut);
    mockApiPost({
      '/api/v1/auth/email/confirm': problem(410, 'the link is invalid or has expired'),
    });

    renderConfirm('tok-old');
    await confirm();

    expect(
      await screen.findByRole('heading', { name: 'This link is invalid or has expired' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Go to your account' })).toHaveAttribute(
      'href',
      '/account',
    );
    expect(screen.queryByRole('button', { name: 'Confirm new address' })).not.toBeInTheDocument();
  });

  it('says when the address was taken in the meantime', async () => {
    mockApiGet(signedOut);
    mockApiPost({
      '/api/v1/auth/email/confirm': problem(409, 'the email address belongs to another account'),
    });

    renderConfirm();
    await confirm();

    expect(await screen.findByText('This address is taken')).toBeInTheDocument();
    expect(
      screen.getByText(
        'Meanwhile, another account uses this email address. Your address remains unchanged.',
      ),
    ).toBeInTheDocument();
  });

  it('treats a link without a token as invalid', async () => {
    mockApiGet(signedOut);

    renderInRouter(<ConfirmEmailChangePage />, { at: '/confirm-email-change' });

    expect(
      await screen.findByRole('heading', { name: 'This link is invalid or has expired' }),
    ).toBeInTheDocument();
  });
});
