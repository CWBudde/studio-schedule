import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AccountPage } from '@/features/account/AccountPage';
import { family, meMember, mockApiGet, mockApiPut, publicSettings } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import type { ProfileProps, ProfileSections, Theme } from '../slots';
import { ThemeContext } from '../themeContext';
import { Profile } from './Profile';

afterEach(() => {
  vi.restoreAllMocks();
});

/** A stand-in for every section: a paragraph naming it. */
const markers = {
  profile: <p>Section profile</p>,
  dependents: <p>Section dependents</p>,
  calendarFeeds: <p>Section calendarFeeds</p>,
  push: <p>Section push</p>,
  myData: <p>Section myData</p>,
  password: <p>Section password</p>,
  email: <p>Section email</p>,
  deleteAccount: <p>Section deleteAccount</p>,
} satisfies ProfileSections;

/** The slot on /account; resolves once the router has rendered it. */
async function renderProfile(props: ProfileProps) {
  renderInRouter(<Profile {...props} />, { at: '/account' });
  return screen.findByRole('heading', { level: 1, name: 'My details' });
}

/** The marker sections on the page, in document order. */
const shownSections = () => screen.queryAllByText(/^Section /).map((p) => p.textContent);

describe('Werkstatt Profile', () => {
  it('lays out every section in Programme’s order under the "My details" heading', async () => {
    await renderProfile({ state: 'ready', me: meMember, sections: markers });

    expect(screen.getByRole('heading', { level: 1, name: 'My details' })).toBeInTheDocument();
    expect(
      screen.getByText('Your details, your children and how you sign in.'),
    ).toBeInTheDocument();
    expect(shownSections()).toEqual([
      'Section profile',
      'Section dependents',
      'Section calendarFeeds',
      'Section push',
      'Section myData',
      'Section password',
      'Section email',
      'Section deleteAccount',
    ]);
    expect(screen.queryByText('Signed in as someone else')).not.toBeInTheDocument();
  });

  it('wraps the sections in the class werkstatt.css restyles them through', async () => {
    await renderProfile({ state: 'ready', me: meMember, sections: markers });

    expect(screen.getByText('Section profile').closest('.werkstatt-profile')).not.toBeNull();
  });

  it('hides the credential sections while an admin is signed in as the account, and says why', async () => {
    await renderProfile({
      state: 'ready',
      me: meMember,
      sections: markers,
      impersonating: { name: 'Nora Example' },
    });

    expect(screen.getByText('Signed in as someone else')).toBeInTheDocument();
    expect(
      screen.getByText(
        'While you are signed in as Nora Example, you cannot change their password or email address.',
      ),
    ).toBeInTheDocument();
    expect(shownSections()).toEqual([
      'Section profile',
      'Section dependents',
      'Section calendarFeeds',
      'Section push',
      'Section myData',
    ]);
  });

  it('shows a status while the account loads', async () => {
    await renderProfile({ state: 'loading' });

    expect(screen.getByRole('status')).toHaveTextContent('Loading your account…');
    expect(screen.getByRole('heading', { level: 1, name: 'My details' })).toBeInTheDocument();
    expect(shownSections()).toEqual([]);
  });

  it('shows the error when the account cannot be loaded', async () => {
    await renderProfile({ state: 'error', error: new Error('boom') });

    expect(screen.getByRole('alert')).toHaveTextContent('Something went wrong. Please try again.');
    expect(shownSections()).toEqual([]);
  });

  it('asks to sign in when signed out', async () => {
    await renderProfile({ state: 'signedOut' });

    expect(screen.getByText('Please sign in')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(
      screen.queryByText('Your details, your children and how you sign in.'),
    ).not.toBeInTheDocument();
  });
});

describe('Werkstatt Profile on the account page', () => {
  // Built here rather than imported from ./index.ts, which imports the CSS.
  const werkstattLike: Theme = { id: 'werkstatt', slots: { Profile } };

  const signedInMember = {
    '/api/v1/demo/mailbox': { status: 404, body: undefined },
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: meMember },
    '/api/v1/family': { body: family },
    '/api/v1/my/calendar-feeds': { body: { items: [] } },
  };

  function renderAccount() {
    return renderInRouter(
      <ThemeContext.Provider value={werkstattLike}>
        <AccountPage />
      </ThemeContext.Provider>,
      { at: '/account' },
    );
  }

  it('renders the connected cards of every section', async () => {
    mockApiGet(signedInMember);

    renderAccount();
    expect(
      await screen.findByRole('heading', { level: 1, name: 'My details' }),
    ).toBeInTheDocument();
    await screen.findByRole('region', { name: 'Change email address' });

    const titles = screen
      .getAllByRole('region')
      .map((r) => within(r).getByRole('heading', { level: 2 }).textContent);
    // Push renders nothing in jsdom (no service worker), as on Programme's page.
    expect(titles).toEqual([
      'Your details',
      'Your children',
      'Calendar feeds',
      'Your data',
      'Change password',
      'Change email address',
      'Delete account',
    ]);
    for (const region of screen.getAllByRole('region')) {
      expect(region.closest('.werkstatt-profile')).not.toBeNull();
    }
  });

  it('saves the person data through the same form as Programme’s', async () => {
    mockApiGet(signedInMember);
    const put = mockApiPut({
      '/api/v1/family/me': { body: { ...family.me, phone: '+49 000 0000123' } },
    });
    const user = userEvent.setup();

    renderAccount();
    const card = within(await screen.findByRole('region', { name: 'Your details' }));
    const phone = await card.findByLabelText('Phone (optional)');
    await user.clear(phone);
    await user.type(phone, '+49 000 0000123');
    await user.click(card.getByRole('button', { name: 'Save details' }));

    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/v1/family/me', {
        body: {
          givenName: 'Nora',
          familyName: 'Example',
          phone: '+49 000 0000123',
          birthDate: '1988-06-15',
        },
      }),
    );
  });

  it('keeps the form’s validation: an empty first name is not sent', async () => {
    mockApiGet(signedInMember);
    const put = mockApiPut({});
    const user = userEvent.setup();

    renderAccount();
    const card = within(await screen.findByRole('region', { name: 'Your details' }));
    await user.clear(await card.findByLabelText('First name'));
    await user.click(card.getByRole('button', { name: 'Save details' }));

    expect(await card.findByText('Please fill in this field.')).toBeInTheDocument();
    expect(card.getByLabelText('First name')).toHaveAttribute('aria-invalid', 'true');
    expect(put).not.toHaveBeenCalled();
  });
});
