import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { meQuery } from '@/api/queries';
import {
  demoLogins,
  meMember,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  signedOut,
  tooManyRequests,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { LoginPage } from './LoginPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const notDemo = {
  '/api/v1/demo/logins': { status: 404, body: undefined },
  '/api/v1/demo/mailbox': { status: 404, body: undefined },
};

function renderLogin() {
  return renderInRouter(<LoginPage />, { at: '/login' });
}

async function fillAndSubmit(email: string, password: string) {
  const user = userEvent.setup();
  if (email) await user.type(await screen.findByLabelText('Email'), email);
  if (password) await user.type(screen.getByLabelText('Password'), password);
  await user.click(screen.getByRole('button', { name: 'Sign in' }));
}

describe('LoginPage', () => {
  it('signs in with email and password and goes home', async () => {
    mockApiGet({ ...signedOut, ...notDemo });
    const post = mockApiPost({ '/api/v1/auth/login': { body: meMember } });

    const { router, queryClient } = renderLogin();
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in' })).toBeInTheDocument();

    await fillAndSubmit('nora.example@example.org', 'secret-password');

    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(post).toHaveBeenCalledWith('/api/v1/auth/login', {
      body: { email: 'nora.example@example.org', password: 'secret-password' },
    });
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meMember);
  });

  it('checks the fields before sending', async () => {
    mockApiGet({ ...signedOut, ...notDemo });
    const post = mockApiPost({});

    renderLogin();
    await fillAndSubmit('not-an-email', '');

    expect(await screen.findByText('Please enter a valid email address.')).toBeInTheDocument();
    expect(screen.getByText('Please fill in this field.')).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toHaveAttribute('aria-invalid', 'true');
    expect(post).not.toHaveBeenCalled();
  });

  it('says when the email or password is wrong', async () => {
    mockApiGet({ ...signedOut, ...notDemo });
    mockApiPost({ '/api/v1/auth/login': problem(401, 'invalid email or password') });

    const { router } = renderLogin();
    await fillAndSubmit('nora.example@example.org', 'wrong-password');

    expect(
      await screen.findByText('The email address or password is incorrect.'),
    ).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/login');
  });

  it('asks to confirm the email first, pointing to the demo mailbox if there is one', async () => {
    mockApiGet({
      ...signedOut,
      '/api/v1/demo/logins': { body: { logins: demoLogins } },
      '/api/v1/demo/mailbox': { body: { mails: [] } },
      '/api/v1/settings/public': { body: publicSettings },
    });
    mockApiPost({ '/api/v1/auth/login': problem(403, 'email address not confirmed') });

    renderLogin();
    await fillAndSubmit('new.dancer@example.org', 'secret-password');

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Please confirm your email address first')).toBeInTheDocument();
    expect(
      await within(alert).findByRole('button', { name: 'Open the demo mailbox' }),
    ).toBeInTheDocument();
  });

  it('asks to confirm the email first without a mailbox hint outside a demo', async () => {
    mockApiGet({ ...signedOut, ...notDemo });
    mockApiPost({ '/api/v1/auth/login': problem(403, 'email address not confirmed') });

    renderLogin();
    await fillAndSubmit('new.dancer@example.org', 'secret-password');

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Please confirm your email address first')).toBeInTheDocument();
    expect(within(alert).queryByRole('button')).not.toBeInTheDocument();
  });

  it('lists the demo accounts and signs in with one click', async () => {
    mockApiGet({
      ...signedOut,
      ...notDemo,
      '/api/v1/demo/logins': { body: { logins: demoLogins } },
    });
    const post = mockApiPost({ '/api/v1/auth/login': { body: meMember } });
    const user = userEvent.setup();

    const { router } = renderLogin();

    const list = within(await screen.findByRole('region', { name: 'Demo accounts' }));
    expect(list.getByText('Member')).toBeInTheDocument();
    expect(list.getByText('Instructor')).toBeInTheDocument();
    expect(list.getByText('Office')).toBeInTheDocument();
    expect(list.getByText('Administrator')).toBeInTheDocument();
    expect(list.getByText('Nora Example')).toBeInTheDocument();
    expect(list.getByText('nora.example@example.org')).toBeInTheDocument();
    expect(list.getAllByText('demo-password')).toHaveLength(4);

    await user.click(list.getByRole('button', { name: 'Sign in as Nora Example (Member)' }));

    await waitFor(() => expect(router.state.location.pathname).toBe('/'));
    expect(post).toHaveBeenCalledWith('/api/v1/auth/login', {
      body: { email: 'nora.example@example.org', password: 'demo-password' },
    });
  });

  it('shows no demo accounts outside a demo', async () => {
    const get = mockApiGet({ ...signedOut, ...notDemo });

    renderLogin();

    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in' })).toBeInTheDocument();
    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/demo/logins', expect.anything()));
    expect(screen.queryByRole('region', { name: 'Demo accounts' })).not.toBeInTheDocument();
  });

  it('says how long to wait after too many attempts', async () => {
    mockApiGet({ ...signedOut, ...notDemo });
    mockApiPost({ '/api/v1/auth/login': tooManyRequests(300) });

    renderLogin();
    await fillAndSubmit('nora.example@example.org', 'wrong-password');

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Too many attempts')).toBeInTheDocument();
    expect(within(alert).getByText('Please try again in 5 minutes.')).toBeInTheDocument();
  });

  it('takes the wait from the problem detail without a Retry-After header', async () => {
    mockApiGet({ ...signedOut, ...notDemo });
    mockApiPost({ '/api/v1/auth/login': tooManyRequests(1, { header: false }) });

    renderLogin();
    await fillAndSubmit('nora.example@example.org', 'wrong-password');

    expect(await screen.findByText('Please try again in 1 second.')).toBeInTheDocument();
  });

  it('links to the password reset', async () => {
    mockApiGet({ ...signedOut, ...notDemo });

    renderLogin();

    expect(await screen.findByRole('link', { name: 'Forgot password?' })).toHaveAttribute(
      'href',
      '/forgot-password',
    );
  });

  it('links to the registration', async () => {
    mockApiGet({ ...signedOut, ...notDemo });

    renderLogin();

    expect(await screen.findByRole('link', { name: 'Register' })).toHaveAttribute(
      'href',
      '/register',
    );
  });

  it('says who is signed in already', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meMember } });

    renderLogin();

    expect(await screen.findByText('You are signed in as Nora Example.')).toBeInTheDocument();
  });
});
