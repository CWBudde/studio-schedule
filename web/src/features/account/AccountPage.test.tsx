import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  meImpersonated,
  meMember,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  signedOut,
  tooManyRequests,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { AccountPage } from './AccountPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const notDemo = { '/api/v1/demo/mailbox': { status: 404, body: undefined } };
const signedInMember = { ...notDemo, '/api/v1/auth/me': { body: meMember } };

function renderAccount() {
  return renderInRouter(<AccountPage />, { at: '/account' });
}

const passwordCard = async () =>
  within(await screen.findByRole('region', { name: 'Change password' }));
const emailCard = async () =>
  within(await screen.findByRole('region', { name: 'Change email address' }));

async function changePassword(current: string, next: string, repeat = next) {
  const user = userEvent.setup();
  const card = await passwordCard();
  if (current) await user.type(card.getByLabelText('Current password'), current);
  if (next) await user.type(card.getByLabelText('New password'), next);
  if (repeat) await user.type(card.getByLabelText('Repeat new password'), repeat);
  await user.click(card.getByRole('button', { name: 'Change password' }));
  return card;
}

async function changeEmail(newEmail: string, password: string) {
  const user = userEvent.setup();
  const card = await emailCard();
  if (newEmail) await user.type(card.getByLabelText('New email address'), newEmail);
  if (password) await user.type(card.getByLabelText('Password'), password);
  await user.click(card.getByRole('button', { name: 'Send confirmation link' }));
  return card;
}

describe('AccountPage', () => {
  it('asks to sign in when signed out', async () => {
    mockApiGet({ ...signedOut, ...notDemo });

    renderAccount();

    expect(await screen.findByText('Please sign in')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(screen.queryByRole('region', { name: 'Change password' })).not.toBeInTheDocument();
  });

  it('shows a notice instead of the forms while signed in as someone else', async () => {
    mockApiGet({ ...notDemo, '/api/v1/auth/me': { body: meImpersonated } });

    renderAccount();

    expect(
      await screen.findByText(
        'While you are signed in as Nora Example, you cannot change their password or email address.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Change password' })).not.toBeInTheDocument();
    expect(screen.queryByRole('region', { name: 'Change email address' })).not.toBeInTheDocument();
  });

  describe('change password', () => {
    it('changes the password and says the other devices were signed out', async () => {
      mockApiGet(signedInMember);
      const post = mockApiPost({ '/api/v1/auth/password': { status: 204 } });

      renderAccount();
      expect(
        await screen.findByRole('heading', { level: 1, name: 'Your account' }),
      ).toBeInTheDocument();
      const card = await changePassword('secret-password', 'new-secret-password');

      expect(
        await card.findByText(
          'Your password has been changed. Your other devices have been signed out.',
        ),
      ).toBeInTheDocument();
      expect(post).toHaveBeenCalledWith('/api/v1/auth/password', {
        body: { currentPassword: 'secret-password', newPassword: 'new-secret-password' },
      });
      expect(card.getByLabelText('Current password')).toHaveValue('');
      expect(card.getByLabelText('New password')).toHaveValue('');
    });

    it('checks the fields before sending', async () => {
      mockApiGet(signedInMember);
      const post = mockApiPost({});

      renderAccount();
      const card = await changePassword('', 'short', 'shorter');

      expect(await card.findByText('Please fill in this field.')).toBeInTheDocument();
      expect(card.getByText('The password must have at least 8 characters.')).toBeInTheDocument();
      expect(post).not.toHaveBeenCalled();
    });

    it('requires the same new password twice', async () => {
      mockApiGet(signedInMember);
      const post = mockApiPost({});

      renderAccount();
      const card = await changePassword('secret-password', 'new-secret-password', 'other-password');

      expect(await card.findByText('The passwords do not match.')).toBeInTheDocument();
      expect(post).not.toHaveBeenCalled();
    });

    it('marks a wrong current password at its field', async () => {
      mockApiGet(signedInMember);
      mockApiPost({
        '/api/v1/auth/password': problem(422, 'validation failed', [
          { location: 'body.currentPassword' },
        ]),
      });

      renderAccount();
      const card = await changePassword('wrong-password', 'new-secret-password');

      expect(await card.findByText('The password is incorrect.')).toBeInTheDocument();
      expect(card.getByLabelText('Current password')).toHaveAttribute('aria-invalid', 'true');
      expect(card.getByLabelText('New password')).not.toHaveAttribute('aria-invalid');
    });

    it('says how long to wait after too many wrong passwords', async () => {
      mockApiGet(signedInMember);
      mockApiPost({ '/api/v1/auth/password': tooManyRequests(600) });

      renderAccount();
      const card = await changePassword('wrong-password', 'new-secret-password');

      const alert = within(await card.findByRole('alert'));
      expect(alert.getByText('Too many attempts')).toBeInTheDocument();
      expect(alert.getByText('Please try again in 10 minutes.')).toBeInTheDocument();
    });

    it('explains a refusal while signed in as someone else', async () => {
      mockApiGet(signedInMember);
      mockApiPost({
        '/api/v1/auth/password': problem(403, 'not while signed in as another account'),
      });

      renderAccount();
      const card = await changePassword('secret-password', 'new-secret-password');

      expect(
        await card.findByText('This is not possible while you are signed in as someone else.'),
      ).toBeInTheDocument();
    });
  });

  describe('change email', () => {
    it('shows the current address and sends a link to the new one', async () => {
      mockApiGet(signedInMember);
      const post = mockApiPost({ '/api/v1/auth/email': { status: 202 } });

      renderAccount();
      const before = await emailCard();
      expect(before.getByText('nora.example@example.org')).toBeInTheDocument();
      const card = await changeEmail('nora.new@example.org', 'secret-password');

      const success = within(await card.findByRole('status'));
      expect(success.getByText('Check your inbox')).toBeInTheDocument();
      expect(
        success.getByText(
          'We have sent a link to nora.new@example.org. Your address changes once you open it.',
        ),
      ).toBeInTheDocument();
      expect(post).toHaveBeenCalledWith('/api/v1/auth/email', {
        body: { newEmail: 'nora.new@example.org', password: 'secret-password', locale: 'en' },
      });
      // The current address stays until the link is opened.
      expect(card.getByText('nora.example@example.org')).toBeInTheDocument();
    });

    it('points to the demo mailbox in a demo', async () => {
      mockApiGet({
        '/api/v1/auth/me': { body: meMember },
        '/api/v1/demo/mailbox': { body: { mails: [] } },
        '/api/v1/settings/public': { body: publicSettings },
      });
      mockApiPost({ '/api/v1/auth/email': { status: 202 } });

      renderAccount();
      const card = await changeEmail('nora.new@example.org', 'secret-password');

      expect(
        await within(await card.findByRole('status')).findByRole('button', {
          name: 'Open the demo mailbox',
        }),
      ).toBeInTheDocument();
    });

    it('refuses the current address before sending', async () => {
      mockApiGet(signedInMember);
      const post = mockApiPost({});

      renderAccount();
      const card = await changeEmail('Nora.Example@example.org', 'secret-password');

      expect(await card.findByText('This is your current email address.')).toBeInTheDocument();
      expect(post).not.toHaveBeenCalled();
    });

    it('marks a wrong password at its field', async () => {
      mockApiGet(signedInMember);
      mockApiPost({
        '/api/v1/auth/email': problem(422, 'validation failed', [{ location: 'body.password' }]),
      });

      renderAccount();
      const card = await changeEmail('nora.new@example.org', 'wrong-password');

      expect(await card.findByText('The password is incorrect.')).toBeInTheDocument();
      expect(card.getByLabelText('Password')).toHaveAttribute('aria-invalid', 'true');
      expect(card.getByLabelText('New email address')).not.toHaveAttribute('aria-invalid');
    });

    it('marks an address the API refuses at its field', async () => {
      mockApiGet(signedInMember);
      mockApiPost({
        '/api/v1/auth/email': problem(422, 'validation failed', [{ location: 'body.newEmail' }]),
      });

      renderAccount();
      const card = await changeEmail('nora.new@example.org', 'secret-password');

      expect(await card.findByText('Please check this entry.')).toBeInTheDocument();
      expect(card.getByLabelText('New email address')).toHaveAttribute('aria-invalid', 'true');
    });

    it('says how long to wait after too many requests', async () => {
      mockApiGet(signedInMember);
      mockApiPost({ '/api/v1/auth/email': tooManyRequests(30) });

      renderAccount();
      const card = await changeEmail('nora.new@example.org', 'secret-password');

      expect(await card.findByText('Please try again in 30 seconds.')).toBeInTheDocument();
    });
  });
});
