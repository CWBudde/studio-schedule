import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import { family, meMember, mockApiGet, mockApiPut, problem, publicSettings } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { AccountPage } from './AccountPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const signedInMember = {
  '/api/v1/demo/mailbox': { status: 404, body: undefined },
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/auth/me': { body: meMember },
  '/api/v1/family': { body: family },
};

function renderAccount() {
  // The toaster lives outside the page, as in the app's layout.
  render(<Toaster />);
  return renderInRouter(<AccountPage />, { at: '/account' });
}

const profileCard = async () => within(await screen.findByRole('region', { name: 'Your details' }));

/** Replaces the text of a field (empty `value`: clears it). */
async function fill(field: HTMLElement, value: string) {
  const user = userEvent.setup();
  await user.clear(field);
  if (value) await user.type(field, value);
}

describe('ProfileCard', () => {
  it('shows my person data', async () => {
    mockApiGet(signedInMember);

    renderAccount();
    const card = await profileCard();

    expect(await card.findByLabelText('First name')).toHaveValue('Nora');
    expect(card.getByLabelText('Last name')).toHaveValue('Example');
    expect(card.getByLabelText('Phone (optional)')).toHaveValue('+49 000 0000999');
    expect(card.getByLabelText('Birth date (optional)')).toHaveValue('1988-06-15');
  });

  it('saves my person data and asks again for the name in the header', async () => {
    const get = mockApiGet(signedInMember);
    const put = mockApiPut({
      '/api/v1/family/me': { body: { ...family.me, givenName: 'Norah' } },
    });

    renderAccount();
    const card = await profileCard();
    await fill(await card.findByLabelText('First name'), 'Norah ');
    await userEvent.setup().click(card.getByRole('button', { name: 'Save details' }));

    expect(await screen.findByText('Your details have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/family/me', {
      body: {
        givenName: 'Norah',
        familyName: 'Example',
        phone: '+49 000 0000999',
        birthDate: '1988-06-15',
      },
    });
    const meRequests = () =>
      get.mock.calls.filter((call) => (call as unknown[])[0] === '/api/v1/auth/me').length;
    await waitFor(() => expect(meRequests()).toBe(2));
  });

  it('leaves out an empty phone number and birth date', async () => {
    mockApiGet(signedInMember);
    const put = mockApiPut({ '/api/v1/family/me': { body: family.me } });

    renderAccount();
    const card = await profileCard();
    await fill(await card.findByLabelText('Phone (optional)'), '');
    await fill(card.getByLabelText('Birth date (optional)'), '');
    await userEvent.setup().click(card.getByRole('button', { name: 'Save details' }));

    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/v1/family/me', {
        body: { givenName: 'Nora', familyName: 'Example' },
      }),
    );
  });

  it('checks the fields before sending', async () => {
    mockApiGet(signedInMember);
    const put = mockApiPut({});

    renderAccount();
    const card = await profileCard();
    await fill(await card.findByLabelText('First name'), '');
    await fill(card.getByLabelText('Birth date (optional)'), '2999-01-01');
    await userEvent.setup().click(card.getByRole('button', { name: 'Save details' }));

    expect(await card.findByText('Please fill in this field.')).toBeInTheDocument();
    expect(card.getByText('The birth date cannot be in the future.')).toBeInTheDocument();
    expect(card.getByLabelText('Birth date (optional)')).toHaveAttribute('aria-invalid', 'true');
    expect(put).not.toHaveBeenCalled();
  });

  it('marks a birth date the API refuses as in the future', async () => {
    // Without the settings the school's today is unknown, so the API decides.
    mockApiGet({ ...signedInMember, '/api/v1/settings/public': { status: 500 } });
    mockApiPut({
      '/api/v1/family/me': problem(422, 'validation failed', [{ location: 'body.birthDate' }]),
    });

    renderAccount();
    const card = await profileCard();
    await fill(await card.findByLabelText('Birth date (optional)'), '2999-01-01');
    await userEvent.setup().click(card.getByRole('button', { name: 'Save details' }));

    expect(await card.findByText('The birth date cannot be in the future.')).toBeInTheDocument();
    expect(card.getByText('Please check your entries.')).toBeInTheDocument();
  });

  it('marks a field the API refuses', async () => {
    mockApiGet(signedInMember);
    mockApiPut({
      '/api/v1/family/me': problem(422, 'validation failed', [{ location: 'body.phone' }]),
    });

    renderAccount();
    const card = await profileCard();
    await fill(await card.findByLabelText('Phone (optional)'), '12345');
    await userEvent.setup().click(card.getByRole('button', { name: 'Save details' }));

    expect(await card.findByText('Please check this entry.')).toBeInTheDocument();
    expect(card.getByLabelText('Phone (optional)')).toHaveAttribute('aria-invalid', 'true');
    expect(card.getByLabelText('First name')).not.toHaveAttribute('aria-invalid');
  });

  describe('language for emails', () => {
    async function chooseLanguage(name: string) {
      const user = userEvent.setup();
      const card = await profileCard();
      const select = card.getByRole('combobox', { name: 'Language for emails' });
      await user.click(select);
      await user.click(await screen.findByRole('option', { name }));
      return select;
    }

    it("saves the account's language without changing the UI's", async () => {
      mockApiGet(signedInMember);
      const put = mockApiPut({ '/api/v1/auth/locale': { status: 204 } });

      renderAccount();
      const card = await profileCard();
      expect(card.getByRole('combobox', { name: 'Language for emails' })).toHaveTextContent(
        'School default',
      );
      const select = await chooseLanguage('Deutsch');

      expect(
        await screen.findByText('The language for emails has been saved.'),
      ).toBeInTheDocument();
      expect(put).toHaveBeenCalledWith('/api/v1/auth/locale', { body: { locale: 'de' } });
      expect(select).toHaveTextContent('Deutsch');
      expect(i18n.resolvedLanguage).toBe('en');
    });

    it("goes back to the school's default", async () => {
      mockApiGet({ ...signedInMember, '/api/v1/auth/me': { body: { ...meMember, locale: 'de' } } });
      const put = mockApiPut({ '/api/v1/auth/locale': { status: 204 } });

      renderAccount();
      const card = await profileCard();
      expect(card.getByRole('combobox', { name: 'Language for emails' })).toHaveTextContent(
        'Deutsch',
      );
      const select = await chooseLanguage('School default');

      await waitFor(() => expect(select).toHaveTextContent('School default'));
      expect(put).toHaveBeenCalledWith('/api/v1/auth/locale', { body: {} });
    });

    it('says when saving failed', async () => {
      mockApiGet(signedInMember);
      mockApiPut({ '/api/v1/auth/locale': { status: 500 } });

      renderAccount();
      const select = await chooseLanguage('English');

      expect(
        await (await profileCard()).findByText('Something went wrong. Please try again.'),
      ).toBeInTheDocument();
      expect(select).toHaveTextContent('School default');
    });
  });
});
