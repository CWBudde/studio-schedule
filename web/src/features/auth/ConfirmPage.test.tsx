import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { meQuery } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import { meMember, mockApiGet, mockApiPost, problem } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ConfirmPage } from './ConfirmPage';

afterEach(() => {
  vi.restoreAllMocks();
});

async function submitPassword(password: string) {
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText('Password'), password);
  await user.click(screen.getByRole('button', { name: 'Confirm and sign in' }));
}

describe('ConfirmPage', () => {
  it('confirms the email, signs in and goes home', async () => {
    mockApiGet({});
    const post = mockApiPost({ '/api/v1/auth/confirm': { body: meMember } });

    // The toaster lives outside the page, as in the app's layout.
    render(<Toaster />);
    const { router, queryClient } = renderInRouter(<ConfirmPage />, {
      at: '/confirm',
      search: { token: 'tok-123' },
    });
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Confirm your email address' }),
    ).toBeInTheDocument();

    await submitPassword('secret-password');

    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(post).toHaveBeenCalledWith('/api/v1/auth/confirm', {
      body: { token: 'tok-123', password: 'secret-password' },
    });
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meMember);
    expect(
      await screen.findByText('Your email address is confirmed. Welcome, Nora!'),
    ).toBeInTheDocument();
  });

  it('explains an invalid or expired link and links to the registration', async () => {
    mockApiGet({});
    mockApiPost({ '/api/v1/auth/confirm': problem(410, 'the link is invalid or has expired') });

    renderInRouter(<ConfirmPage />, {
      at: '/confirm',
      search: { token: 'tok-old' },
    });
    await submitPassword('secret-password');

    expect(
      await screen.findByRole('heading', { name: 'This link is invalid or has expired' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Register again' })).toHaveAttribute(
      'href',
      '/register',
    );
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
  });

  it('says when the password is wrong', async () => {
    mockApiGet({});
    mockApiPost({ '/api/v1/auth/confirm': problem(401, 'invalid email or password') });

    const { router } = renderInRouter(<ConfirmPage />, {
      at: '/confirm',
      search: { token: 'tok-123' },
    });
    await submitPassword('wrong-password');

    expect(await screen.findByText('The password is incorrect.')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/confirm');
  });

  it('requires a password', async () => {
    mockApiGet({});
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderInRouter(<ConfirmPage />, {
      at: '/confirm',
      search: { token: 'tok-123' },
    });
    await user.click(await screen.findByRole('button', { name: 'Confirm and sign in' }));

    expect(await screen.findByText('Please fill in this field.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('treats a link without a token as invalid', async () => {
    mockApiGet({});

    renderInRouter(<ConfirmPage />, { at: '/confirm' });

    expect(
      await screen.findByRole('heading', { name: 'This link is invalid or has expired' }),
    ).toBeInTheDocument();
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
  });
});
