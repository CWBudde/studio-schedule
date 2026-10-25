import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { mockApiGet, mockApiPost, problem, publicSettings, tooManyRequests } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { RegisterPage } from './RegisterPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const notDemo = { '/api/v1/demo/mailbox': { status: 404, body: undefined } };
const demo = {
  '/api/v1/demo/mailbox': { body: { mails: [] } },
  '/api/v1/settings/public': { body: publicSettings },
};

const registration = {
  givenName: 'Nina',
  familyName: 'Dancer',
  email: 'new.dancer@example.org',
  password: 'secret-password',
};

async function fillAndSubmit(fields: Partial<typeof registration>, labels = englishLabels) {
  const user = userEvent.setup();
  for (const [key, label] of Object.entries(labels.fields)) {
    const value = fields[key as keyof typeof registration];
    if (value) await user.type(await screen.findByLabelText(label), value);
  }
  await user.click(screen.getByRole('button', { name: labels.submit }));
}

const englishLabels = {
  fields: {
    givenName: 'First name',
    familyName: 'Last name',
    email: 'Email',
    password: 'Password',
  },
  submit: 'Register',
};

describe('RegisterPage', () => {
  it('registers and asks to check the inbox', async () => {
    mockApiGet(notDemo);
    const post = mockApiPost({ '/api/v1/auth/register': { status: 202 } });

    renderInRouter(<RegisterPage />, { at: '/register' });
    expect(await screen.findByRole('heading', { level: 1, name: 'Register' })).toBeInTheDocument();

    await fillAndSubmit(registration);

    const success = await screen.findByRole('status');
    expect(within(success).getByText('Check your inbox')).toBeInTheDocument();
    expect(within(success).getByText(/new\.dancer@example\.org/)).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/auth/register', {
      body: { ...registration, locale: 'en' },
    });
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
    expect(within(success).queryByText(/demo mailbox/)).not.toBeInTheDocument();
  });

  it('points to the demo mailbox in a demo', async () => {
    mockApiGet(demo);
    mockApiPost({ '/api/v1/auth/register': { status: 202 } });

    renderInRouter(<RegisterPage />, { at: '/register' });
    await fillAndSubmit(registration);

    const success = await screen.findByRole('status');
    expect(
      await within(success).findByText(
        'This demo sends no real email. You find the mail in the demo mailbox.',
      ),
    ).toBeInTheDocument();
    expect(
      within(success).getByRole('button', { name: 'Open the demo mailbox' }),
    ).toBeInTheDocument();
  });

  it('sends the UI language for the confirmation mail', async () => {
    await i18n.changeLanguage('de');
    mockApiGet(notDemo);
    const post = mockApiPost({ '/api/v1/auth/register': { status: 202 } });

    renderInRouter(<RegisterPage />, { at: '/register' });
    await fillAndSubmit(registration, {
      fields: {
        givenName: 'Vorname',
        familyName: 'Nachname',
        email: 'E-Mail',
        password: 'Passwort',
      },
      submit: 'Registrieren',
    });

    expect(await screen.findByText('Bitte prüfe dein Postfach')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/auth/register', {
      body: { ...registration, locale: 'de' },
    });
  });

  it('checks the fields before sending', async () => {
    mockApiGet(notDemo);
    const post = mockApiPost({});

    renderInRouter(<RegisterPage />, { at: '/register' });
    await fillAndSubmit({ email: 'new.dancer@example.org', password: 'short' });

    expect(
      await screen.findByText('The password must have at least 8 characters.'),
    ).toBeInTheDocument();
    expect(screen.getAllByText('Please fill in this field.')).toHaveLength(2);
    expect(screen.getByLabelText('First name')).toHaveAttribute('aria-invalid', 'true');
    expect(post).not.toHaveBeenCalled();
  });

  it('rates the password while it is typed and says when the API finds it too common', async () => {
    mockApiGet(notDemo);
    mockApiPost({
      '/api/v1/auth/register': problem(422, 'validation failed', [{ location: 'body.password' }]),
    });

    renderInRouter(<RegisterPage />, { at: '/register' });
    await fillAndSubmit({ ...registration, password: 'abcdefgh' });

    expect(screen.getByText('Password strength: weak')).toBeInTheDocument();
    expect(
      await screen.findByText('This password is too common. Please choose another one.'),
    ).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toHaveAttribute('aria-invalid', 'true');
  });

  it('shows the errors the API finds at their fields', async () => {
    mockApiGet(notDemo);
    mockApiPost({
      '/api/v1/auth/register': problem(422, 'validation failed', [{ location: 'body.email' }]),
    });

    renderInRouter(<RegisterPage />, { at: '/register' });
    await fillAndSubmit(registration);

    expect(await screen.findByText('Please check your entries.')).toBeInTheDocument();
    expect(screen.getByText('Please check this entry.')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByLabelText('Email')).toHaveAttribute('aria-invalid', 'true'),
    );
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
  });

  it('says how long to wait after too many attempts', async () => {
    mockApiGet(notDemo);
    mockApiPost({ '/api/v1/auth/register': tooManyRequests(90) });

    renderInRouter(<RegisterPage />, { at: '/register' });
    await fillAndSubmit(registration);

    const alert = await screen.findByRole('alert');
    expect(within(alert).getByText('Too many attempts')).toBeInTheDocument();
    expect(within(alert).getByText('Please try again in 90 seconds.')).toBeInTheDocument();
  });

  it('links to the sign-in page', async () => {
    mockApiGet(notDemo);

    renderInRouter(<RegisterPage />, { at: '/register' });

    expect(await screen.findByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
  });
});
