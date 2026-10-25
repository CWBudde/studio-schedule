import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mockApiGet, mockApiPost, problem } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ResetPasswordPage } from './ResetPasswordPage';

afterEach(() => {
  vi.restoreAllMocks();
});

function renderReset(token = 'tok-reset') {
  return renderInRouter(<ResetPasswordPage />, { at: '/reset-password', search: { token } });
}

async function setPassword(password: string, repeat = password) {
  const user = userEvent.setup();
  if (password) await user.type(await screen.findByLabelText('New password'), password);
  if (repeat) await user.type(screen.getByLabelText('Repeat password'), repeat);
  await user.click(screen.getByRole('button', { name: 'Set password' }));
}

describe('ResetPasswordPage', () => {
  it('sets the new password and links to the sign-in', async () => {
    mockApiGet({});
    const post = mockApiPost({ '/api/v1/auth/password-reset/confirm': { status: 204 } });

    renderReset();
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Set a new password' }),
    ).toBeInTheDocument();

    await setPassword('new-secret-password');

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Your password has been changed' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(post).toHaveBeenCalledWith('/api/v1/auth/password-reset/confirm', {
      body: { token: 'tok-reset', password: 'new-secret-password' },
    });
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument();
  });

  it('requires the same password twice', async () => {
    mockApiGet({});
    const post = mockApiPost({});

    renderReset();
    await setPassword('new-secret-password', 'new-secret-passwort');

    expect(await screen.findByText('The passwords do not match.')).toBeInTheDocument();
    expect(screen.getByLabelText('Repeat password')).toHaveAttribute('aria-invalid', 'true');
    expect(post).not.toHaveBeenCalled();
  });

  it('requires at least 8 characters', async () => {
    mockApiGet({});
    const post = mockApiPost({});

    renderReset();
    await setPassword('short');

    expect(
      await screen.findByText('The password must have at least 8 characters.'),
    ).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('says when the API refuses the password as too common', async () => {
    mockApiGet({});
    mockApiPost({
      '/api/v1/auth/password-reset/confirm': problem(422, 'validation failed', [
        { location: 'body.password' },
      ]),
    });

    renderReset();
    await setPassword('new-secret-password');

    expect(await screen.findByText('Please check your entries.')).toBeInTheDocument();
    expect(
      screen.getByText('This password is too common. Please choose another one.'),
    ).toBeInTheDocument();
    expect(screen.getByLabelText('New password')).toHaveAttribute('aria-invalid', 'true');
  });

  it('explains an invalid or expired link and links to a new request', async () => {
    mockApiGet({});
    mockApiPost({
      '/api/v1/auth/password-reset/confirm': problem(410, 'the link is invalid or has expired'),
    });

    renderReset('tok-old');
    await setPassword('new-secret-password');

    expect(
      await screen.findByRole('heading', { name: 'This link is invalid or has expired' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Request a new link' })).toHaveAttribute(
      'href',
      '/forgot-password',
    );
    expect(screen.queryByLabelText('New password')).not.toBeInTheDocument();
  });

  it('treats a link without a token as invalid', async () => {
    mockApiGet({});

    renderInRouter(<ResetPasswordPage />, { at: '/reset-password' });

    expect(
      await screen.findByRole('heading', { name: 'This link is invalid or has expired' }),
    ).toBeInTheDocument();
  });
});
