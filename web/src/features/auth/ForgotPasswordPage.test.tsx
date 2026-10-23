import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mockApiGet, mockApiPost, problem, publicSettings, tooManyRequests } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ForgotPasswordPage } from './ForgotPasswordPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const notDemo = { '/api/v1/demo/mailbox': { status: 404, body: undefined } };
const demo = {
  '/api/v1/demo/mailbox': { body: { mails: [] } },
  '/api/v1/settings/public': { body: publicSettings },
};

async function requestLink(email: string) {
  const user = userEvent.setup();
  if (email) await user.type(await screen.findByLabelText('Email'), email);
  await user.click(screen.getByRole('button', { name: 'Send link' }));
}

describe('ForgotPasswordPage', () => {
  it('requests a reset link in the UI language and asks to check the inbox', async () => {
    mockApiGet(notDemo);
    const post = mockApiPost({ '/api/v1/auth/password-reset': { status: 202 } });

    renderInRouter(<ForgotPasswordPage />, { at: '/forgot-password' });
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Forgot your password?' }),
    ).toBeInTheDocument();

    await requestLink('nora.example@example.org');

    const success = await screen.findByRole('status');
    expect(within(success).getByText('Check your inbox')).toBeInTheDocument();
    expect(within(success).getByText(/nora\.example@example\.org/)).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/auth/password-reset', {
      body: { email: 'nora.example@example.org', locale: 'en' },
    });
    expect(screen.queryByLabelText('Email')).not.toBeInTheDocument();
  });

  it('points to the demo mailbox in a demo', async () => {
    mockApiGet(demo);
    mockApiPost({ '/api/v1/auth/password-reset': { status: 202 } });

    renderInRouter(<ForgotPasswordPage />, { at: '/forgot-password' });
    await requestLink('nora.example@example.org');

    const success = await screen.findByRole('status');
    expect(
      await within(success).findByRole('button', { name: 'Open the demo mailbox' }),
    ).toBeInTheDocument();
  });

  it('checks the address before sending', async () => {
    mockApiGet(notDemo);
    const post = mockApiPost({});

    renderInRouter(<ForgotPasswordPage />, { at: '/forgot-password' });
    await requestLink('not-an-email');

    expect(await screen.findByText('Please enter a valid email address.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('says how long to wait after too many requests', async () => {
    mockApiGet(notDemo);
    mockApiPost({ '/api/v1/auth/password-reset': tooManyRequests(42) });

    renderInRouter(<ForgotPasswordPage />, { at: '/forgot-password' });
    await requestLink('nora.example@example.org');

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Too many attempts')).toBeInTheDocument();
    expect(within(alert).getByText('Please try again in 42 seconds.')).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toHaveValue('nora.example@example.org');
  });

  it('shows the errors the API finds at their fields', async () => {
    mockApiGet(notDemo);
    mockApiPost({
      '/api/v1/auth/password-reset': problem(422, 'validation failed', [
        { location: 'body.email' },
      ]),
    });

    renderInRouter(<ForgotPasswordPage />, { at: '/forgot-password' });
    await requestLink('nora.example@example.org');

    expect(await screen.findByText('Please check this entry.')).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toHaveAttribute('aria-invalid', 'true');
  });

  it('links back to the sign-in page', async () => {
    mockApiGet(notDemo);

    renderInRouter(<ForgotPasswordPage />, { at: '/forgot-password' });

    expect(await screen.findByRole('link', { name: 'Back to sign-in' })).toHaveAttribute(
      'href',
      '/login',
    );
  });
});
