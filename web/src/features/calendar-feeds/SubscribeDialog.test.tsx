import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarFeed, NewCalendarFeed } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  ids,
  meImpersonated,
  meMember,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  problem,
  publicSettings,
  signedOut,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { SubscribeButton } from './SubscribeDialog';
import type { FeedSubject } from './subject';

afterEach(() => {
  vi.restoreAllMocks();
});

const feedId = '0190a000-0000-7000-8000-000000000601';

/** The family's bookings feed, created Tuesday 2026-10-20 and last fetched Saturday 2026-10-24 (Berlin). */
const bookingsFeed: CalendarFeed = {
  id: feedId,
  scope: 'bookings',
  createdAt: '2026-10-20T10:00:00Z',
  lastUsedAt: '2026-10-24T05:00:00Z',
};

const createdFeed = (feed: Partial<CalendarFeed> = {}, url?: string): NewCalendarFeed => ({
  feed: {
    id: '0190a000-0000-7000-8000-000000000602',
    scope: 'bookings',
    ...feed,
    createdAt: '2026-10-25T09:00:00Z',
  },
  token: 'tok-secret',
  path: '/ical/tok-secret.ics',
  ...(url ? { url } : {}),
});

/** Mocks a signed-in `me` (or the fixtures given) with the active feeds of `feeds`. */
function mockFeeds(feeds: CalendarFeed[], me: object = { '/api/v1/auth/me': { body: meMember } }) {
  mockApiGet({
    ...me,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/my/calendar-feeds': () => ({ body: { items: feeds } }),
  });
}

function renderSubscribe(subject: FeedSubject = { scope: 'bookings' }) {
  render(<Toaster />);
  return renderInRouter(<SubscribeButton subject={subject} />, { at: '/my-lessons' });
}

async function openDialog(name = 'Your lessons in your calendar') {
  const user = userEvent.setup();
  await user.click(await screen.findByRole('button', { name: 'Subscribe' }));
  const dialog = await screen.findByRole('dialog', { name });
  return { user, dialog: within(dialog) };
}

describe('SubscribeButton', () => {
  it('explains the feed, creates a link and shows it once to copy, open and download', async () => {
    mockFeeds([]);
    const url = 'https://studio.example.org/ical/tok-secret.ics';
    const post = mockApiPost({
      '/api/v1/my/calendar-feeds': { status: 201, body: createdFeed({}, url) },
    });
    const open = vi.spyOn(window, 'open').mockReturnValue(null);

    renderSubscribe();
    const { user, dialog } = await openDialog();

    expect(dialog.getByText(/lessons you and your family are booked for/)).toBeInTheDocument();
    expect(dialog.getByText(/from four weeks back on/)).toBeInTheDocument();
    expect(dialog.getByText(/The link is secret/)).toBeInTheDocument();
    expect(await dialog.findByText('You have no link for this calendar yet.')).toBeInTheDocument();
    expect(dialog.queryByText(/current link then stops working/)).not.toBeInTheDocument();

    await user.click(dialog.getByRole('button', { name: 'Create link' }));

    expect(post).toHaveBeenCalledWith('/api/v1/my/calendar-feeds', {
      body: { scope: 'bookings' },
    });
    const created = within(await dialog.findByRole('region', { name: 'Your new link' }));
    expect(created.getByRole('heading', { name: 'Your new link' })).toHaveFocus();
    expect(created.getByText(/shown only this once/)).toBeInTheDocument();
    expect(created.getByRole('textbox', { name: 'Calendar link' })).toHaveValue(url);
    expect(created.getByRole('link', { name: 'Open in calendar app' })).toHaveAttribute(
      'href',
      'webcal://studio.example.org/ical/tok-secret.ics',
    );

    await user.click(created.getByRole('button', { name: 'Copy link' }));
    expect(await navigator.clipboard.readText()).toBe(url);
    expect(await screen.findByText('The link has been copied.')).toBeInTheDocument();

    await user.click(created.getByRole('button', { name: 'Download .ics' }));
    expect(open).toHaveBeenCalledWith(`${url}?download=true`, '_blank', 'noopener');
    // Made once, the link is not offered again.
    expect(dialog.queryByRole('button', { name: /Create/ })).not.toBeInTheDocument();
  });

  it('subscribes to the page origin when the server does not know its URL', async () => {
    mockFeeds([]);
    mockApiPost({ '/api/v1/my/calendar-feeds': { status: 201, body: createdFeed() } });

    renderSubscribe();
    const { user, dialog } = await openDialog();
    await user.click(await dialog.findByRole('button', { name: 'Create link' }));

    expect(await dialog.findByRole('textbox', { name: 'Calendar link' })).toHaveValue(
      `${window.location.origin}/ical/tok-secret.ics`,
    );
  });

  it('shows an existing link with its dates and warns before replacing it', async () => {
    mockFeeds([bookingsFeed]);
    mockApiPost({ '/api/v1/my/calendar-feeds': { status: 201, body: createdFeed() } });

    renderSubscribe();
    const { user, dialog } = await openDialog();

    expect(
      await dialog.findByText('You have a link for this calendar, created on October 20, 2026.'),
    ).toBeInTheDocument();
    expect(dialog.getByText('A calendar last fetched it on October 24, 2026.')).toBeInTheDocument();
    expect(dialog.getByText(/cannot be shown again/)).toBeInTheDocument();
    expect(dialog.getByText(/current link then stops working/)).toBeInTheDocument();

    await user.click(dialog.getByRole('button', { name: 'Create a new link' }));
    expect(await dialog.findByRole('region', { name: 'Your new link' })).toBeInTheDocument();
  });

  it('says when a link has never been fetched', async () => {
    mockFeeds([{ ...bookingsFeed, lastUsedAt: undefined }]);

    renderSubscribe();
    const { dialog } = await openDialog();

    expect(await dialog.findByText('No calendar has fetched it yet.')).toBeInTheDocument();
  });

  it('ignores the links of other subjects', async () => {
    mockFeeds([
      { ...bookingsFeed, scope: 'schedule' },
      {
        ...bookingsFeed,
        scope: 'schedule',
        locationId: ids.locationSouth,
        locationName: 'South Studio',
      },
    ]);

    renderSubscribe({
      scope: 'schedule',
      location: { id: ids.locationNorth, name: 'North Studio' },
    });
    const { dialog } = await openDialog('North Studio in your calendar');

    expect(await dialog.findByText('You have no link for this calendar yet.')).toBeInTheDocument();
    expect(dialog.getByText(/schedule of North Studio/)).toBeInTheDocument();
  });

  it('revokes the link after asking', async () => {
    let feeds = [bookingsFeed];
    mockApiGet({
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/my/calendar-feeds': () => ({ body: { items: feeds } }),
    });
    const revoke = mockApiDelete({
      '/api/v1/my/calendar-feeds/{feedId}': (init: MockRequest) => {
        feeds = feeds.filter((f) => f.id !== init.params?.path?.feedId);
        return { status: 204 };
      },
    });

    renderSubscribe();
    const { user, dialog } = await openDialog();
    await user.click(await dialog.findByRole('button', { name: 'Revoke link' }));

    const confirm = within(dialog.getByRole('group', { name: 'Revoke this link?' }));
    expect(confirm.getByRole('button', { name: 'Keep' })).toHaveFocus();
    await user.click(confirm.getByRole('button', { name: 'Revoke' }));

    expect(revoke).toHaveBeenCalledWith('/api/v1/my/calendar-feeds/{feedId}', {
      params: { path: { feedId } },
    });
    expect(await screen.findByText('The link has been revoked.')).toBeInTheDocument();
    expect(await dialog.findByText('You have no link for this calendar yet.')).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Create link' })).toBeInTheDocument();
  });

  it('explains a refused teaching feed', async () => {
    mockFeeds([], {
      '/api/v1/auth/me': { body: { ...meMember, role: 'instructor', teaches: true } },
    });
    mockApiPost({ '/api/v1/my/calendar-feeds': problem(403, 'no staff record') });

    renderSubscribe({ scope: 'teaching' });
    const { user, dialog } = await openDialog('Your teaching in your calendar');
    await user.click(await dialog.findByRole('button', { name: 'Create link' }));

    expect(
      await dialog.findByText(/not linked to a staff record. Subscribe to the schedule instead./),
    ).toBeInTheDocument();
  });

  it('offers no teaching feed to an account that does not teach', async () => {
    mockFeeds([]);

    renderSubscribe({ scope: 'teaching' });
    const { dialog } = await openDialog('Your teaching in your calendar');

    expect(await dialog.findByText(/not linked to a staff record/)).toBeInTheDocument();
    expect(dialog.queryByRole('button', { name: 'Create link' })).not.toBeInTheDocument();
  });

  it('does not blame an impersonation for a 403 of someone signed in as themselves', async () => {
    mockFeeds([]);
    mockApiPost({ '/api/v1/my/calendar-feeds': problem(403, 'forbidden') });

    renderSubscribe();
    const { user, dialog } = await openDialog();
    await user.click(await dialog.findByRole('button', { name: 'Create link' }));

    expect(await dialog.findByText('Something went wrong. Please try again.')).toBeInTheDocument();
    expect(dialog.queryByText(/signed in as someone else/)).not.toBeInTheDocument();
  });

  it('treats a link revoked meanwhile as revoked', async () => {
    let feeds = [bookingsFeed];
    mockApiGet({
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/my/calendar-feeds': () => ({ body: { items: feeds } }),
    });
    mockApiDelete({
      '/api/v1/my/calendar-feeds/{feedId}': () => {
        feeds = [];
        return problem(404, 'not found');
      },
    });

    renderSubscribe();
    const { user, dialog } = await openDialog();
    await user.click(await dialog.findByRole('button', { name: 'Revoke link' }));
    await user.click(dialog.getByRole('button', { name: 'Revoke' }));

    expect(
      await screen.findByText('This link had already been revoked or replaced.'),
    ).toBeInTheDocument();
    expect(await dialog.findByText('You have no link for this calendar yet.')).toBeInTheDocument();
    expect(dialog.queryByRole('group', { name: 'Revoke this link?' })).not.toBeInTheDocument();
    expect(dialog.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('forgets an open confirmation when its link goes', async () => {
    let feeds = [bookingsFeed];
    mockApiGet({
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/my/calendar-feeds': () => ({ body: { items: feeds } }),
    });

    const { queryClient } = renderSubscribe();
    const { user, dialog } = await openDialog();
    await user.click(await dialog.findByRole('button', { name: 'Revoke link' }));
    expect(dialog.getByRole('group', { name: 'Revoke this link?' })).toBeInTheDocument();

    // Replaced elsewhere (another tab): a new link of the same subject.
    feeds = [{ ...bookingsFeed, id: '0190a000-0000-7000-8000-000000000604' }];
    await queryClient.invalidateQueries({ queryKey: ['calendar-feeds'] });

    expect(await dialog.findByRole('button', { name: 'Revoke link' })).toBeInTheDocument();
    expect(dialog.queryByRole('group', { name: 'Revoke this link?' })).not.toBeInTheDocument();
  });

  it('does not call a fetched link unused while the school time zone is unknown', async () => {
    mockApiGet({
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/settings/public': problem(500, 'down'),
      '/api/v1/my/calendar-feeds': { body: { items: [bookingsFeed] } },
    });

    renderSubscribe();
    const { dialog } = await openDialog();

    expect(await dialog.findByText(/cannot be shown again/)).toBeInTheDocument();
    expect(dialog.queryByText('No calendar has fetched it yet.')).not.toBeInTheDocument();
  });

  it('creates no link while an admin is signed in as the account', async () => {
    mockFeeds([bookingsFeed], { '/api/v1/auth/me': { body: meImpersonated } });

    renderSubscribe();
    const { dialog } = await openDialog();

    expect(
      await dialog.findByText(/While you are signed in as someone else, you cannot create/),
    ).toBeInTheDocument();
    expect(dialog.queryByRole('button', { name: /Create/ })).not.toBeInTheDocument();
    // Revoking stays possible.
    expect(dialog.getByRole('button', { name: 'Revoke link' })).toBeInTheDocument();
  });

  it('asks visitors to sign in or register', async () => {
    mockFeeds([], signedOut);

    renderSubscribe({ scope: 'schedule' });
    const { dialog } = await openDialog('The schedule in your calendar');

    expect(await dialog.findByText('Sign in to subscribe')).toBeInTheDocument();
    expect(dialog.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(dialog.getByRole('link', { name: 'Create account' })).toHaveAttribute(
      'href',
      '/register',
    );
    expect(dialog.queryByRole('button', { name: 'Create link' })).not.toBeInTheDocument();
  });

  it('speaks German', async () => {
    await i18n.changeLanguage('de');
    mockFeeds([bookingsFeed]);

    renderSubscribe();
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Abonnieren' }));
    const dialog = within(
      await screen.findByRole('dialog', { name: 'Deine Stunden in deinem Kalender' }),
    );

    expect(
      await dialog.findByText(
        'Du hast einen Link für diesen Kalender, erstellt am 20. Oktober 2026.',
      ),
    ).toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Neuen Link erstellen' })).toBeInTheDocument();
    await waitFor(() =>
      expect(dialog.getByRole('button', { name: 'Link widerrufen' })).toBeInTheDocument(),
    );
  });
});
