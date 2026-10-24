import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { focusManager, onlineManager } from '@tanstack/react-query';
import { type WaitlistInvite, waitlistInviteQuery } from '@/api/queries';
import i18n from '@/i18n';
import {
  conflict,
  lessonWorkshop,
  type MockRequest,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  signedOut,
  tooManyRequests,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { WaitlistAcceptPage } from './WaitlistAcceptPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const token = 'tok-invite';

/** An invite of Lina for the jazz workshop (Saturday 14:00–17:00 in Berlin), until Friday 22:00. */
const invite: WaitlistInvite = {
  state: 'invited',
  givenName: 'Lina',
  expiresAt: '2026-10-23T20:00:00Z',
  lesson: {
    id: lessonWorkshop.id,
    courseType: lessonWorkshop.courseType,
    room: lessonWorkshop.room,
    location: lessonWorkshop.location,
    instructor: lessonWorkshop.instructor,
    startsAt: lessonWorkshop.startsAt,
    endsAt: lessonWorkshop.endsAt,
    status: 'scheduled',
    kind: 'workshop',
    capacity: 20,
    visibility: 'public',
  },
};

/** The API with the invite `current()` (read on every request), for a visitor. */
function mockInvite(current: () => { status?: number; body?: unknown } = () => ({ body: invite })) {
  return mockApiGet({
    ...signedOut,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/waitlist-invites/{token}': (init: MockRequest) =>
      init.params?.path?.token === token ? current() : problem(404, 'not found'),
  });
}

function renderPage(search: Record<string, string | undefined> = { token }) {
  return renderInRouter(<WaitlistAcceptPage />, { at: '/waitlist/accept', search });
}

describe('WaitlistAcceptPage', () => {
  it('shows the lesson, the person and the expiry in school time, and books nothing on load', async () => {
    const get = mockInvite();
    const post = mockApiPost({});

    renderPage();

    expect(
      await screen.findByText('A place is reserved for Lina until Fri, Oct 23, 22:00.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Waiting list invite' })).toBeVisible();
    expect(screen.getByRole('heading', { level: 2, name: 'Jazz' })).toBeInTheDocument();
    expect(screen.getByText('Saturday, November 7, 14:00–17:00')).toBeInTheDocument();
    expect(screen.getByText('Room A · North Studio')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Accept the place' })).toBeEnabled();
    expect(get).toHaveBeenCalledWith('/api/v1/waitlist-invites/{token}', {
      params: { path: { token } },
      signal: expect.any(AbortSignal),
    });
    expect(post).not.toHaveBeenCalled();
  });

  it('accepts the place with a click', async () => {
    mockInvite();
    const post = mockApiPost({
      '/api/v1/waitlist-invites/{token}': { body: { ...invite, state: 'accepted' } },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Accept the place' }));

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Booked — see you there' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent(
      'Lina is booked for this lesson. A confirmation is on its way by email.',
    );
    expect(post).toHaveBeenCalledWith('/api/v1/waitlist-invites/{token}', {
      params: { path: { token } },
    });
    expect(screen.getByRole('link', { name: 'To the schedule' })).toHaveAttribute(
      'href',
      '/schedule',
    );
  });

  it('shows the state of the invite after a refused accept', async () => {
    let current = invite;
    mockInvite(() => ({ body: current }));
    mockApiPost({
      '/api/v1/waitlist-invites/{token}': () => {
        current = { ...invite, state: 'expired' };
        return conflict('invite-expired');
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Accept the place' }));

    expect(
      await screen.findByRole('heading', { level: 1, name: 'This invite has run out' }),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Accept the place' })).not.toBeInTheDocument();
  });

  it('says when accepting is refused for another reason', async () => {
    mockInvite();
    mockApiPost({ '/api/v1/waitlist-invites/{token}': problem(500, 'boom') });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Accept the place' }));

    expect(await screen.findByText('That did not work. Please try again.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Accept the place' })).toBeEnabled();
  });

  it.each([
    ['accepted', 'Booked — see you there', 'Lina is booked for this lesson.'],
    [
      'expired',
      'This invite has run out',
      'The place was reserved for Lina until Fri, Oct 23, 22:00 and can no longer be taken.',
    ],
    [
      'withdrawn',
      'This invite is no longer valid',
      'Lina is no longer on the waiting list for this lesson.',
    ],
  ] as const)('shows an invite that is %s', async (state, title, text) => {
    mockInvite(() => ({ body: { ...invite, state } }));

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: title })).toBeInTheDocument();
    expect(screen.getByText(text, { exact: false })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'Jazz' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Accept the place' })).not.toBeInTheDocument();
  });

  it.each([
    ['cutoff', 'Booking has closed for this lesson.'],
    ['release', 'This lesson cannot be booked yet.'],
    ['canceled', 'This lesson is canceled and cannot be booked.'],
    ['started', 'This lesson has started and cannot be booked any more.'],
  ] as const)('says why a closed lesson cannot be taken (%s)', async (closed, text) => {
    mockInvite(() => ({ body: { ...invite, closed } }));

    renderPage();

    expect(
      await screen.findByRole('heading', {
        level: 1,
        name: 'This lesson cannot be booked any more',
      }),
    ).toBeInTheDocument();
    expect(screen.getByText(text)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Accept the place' })).not.toBeInTheDocument();
  });

  it('says when the invite ran out but the person is back on the list', async () => {
    mockInvite(() => ({
      body: { ...invite, state: 'waiting', expiresAt: undefined, position: 2 },
    }));

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Your invite has run out' }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        'The place is gone, but Lina is still on the waiting list, position 2. A new invite comes by email when a place frees up.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Accept the place' })).not.toBeInTheDocument();
  });

  it('asks for the invite once and keeps it when a later lookup fails', async () => {
    let answer: { status?: number; body?: unknown; headers?: Record<string, string> } = {
      body: invite,
    };
    const get = mockInvite(() => answer);

    const { queryClient } = renderPage();
    expect(await screen.findByRole('button', { name: 'Accept the place' })).toBeEnabled();
    // Coming back to the tab asks nothing.
    focusManager.setFocused(false);
    focusManager.setFocused(true);
    onlineManager.setOnline(false);
    onlineManager.setOnline(true);
    const lookups = () =>
      get.mock.calls.filter((call) => (call as unknown[])[0] === '/api/v1/waitlist-invites/{token}')
        .length;
    expect(lookups()).toBe(1);

    // A failed lookup later leaves the loaded invite on the page.
    answer = tooManyRequests(60);
    await queryClient.refetchQueries({ queryKey: waitlistInviteQuery(token).queryKey });
    expect(lookups()).toBe(2);
    expect(screen.getByRole('button', { name: 'Accept the place' })).toBeEnabled();
    expect(screen.queryByText('Too many attempts')).not.toBeInTheDocument();
    focusManager.setFocused(undefined);
  });

  it('says when the settings cannot be loaded and tries again', async () => {
    let settingsOk = false;
    mockApiGet({
      ...signedOut,
      '/api/v1/settings/public': () =>
        settingsOk ? { body: publicSettings } : problem(500, 'boom'),
      '/api/v1/waitlist-invites/{token}': { body: invite },
    });
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByText('The invite could not be loaded.')).toBeInTheDocument();
    settingsOk = true;
    await user.click(screen.getByRole('button', { name: 'Try again' }));

    expect(await screen.findByRole('button', { name: 'Accept the place' })).toBeEnabled();
  });

  it('says when the link is unknown', async () => {
    mockInvite(() => problem(404, 'not found'));

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Unknown link' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'To the schedule' })).toBeInTheDocument();
  });

  it('says when the link has no token, without asking the API', async () => {
    const get = mockInvite();

    renderPage({});

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Unknown link' }),
    ).toBeInTheDocument();
    expect(get).not.toHaveBeenCalledWith('/api/v1/waitlist-invites/{token}', expect.anything());
  });

  it('says how long to wait after too many lookups', async () => {
    mockInvite(() => tooManyRequests(120));

    renderPage();

    expect(await screen.findByText('Too many attempts')).toBeInTheDocument();
    expect(screen.getByText('Please try again in 2 minutes.')).toBeInTheDocument();
  });

  it('says how long to wait after too many accepts', async () => {
    mockInvite();
    mockApiPost({ '/api/v1/waitlist-invites/{token}': tooManyRequests(30) });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Accept the place' }));

    expect(await screen.findByText('Too many attempts')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Accept the place' })).toBeEnabled();
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    try {
      mockInvite();

      renderPage();

      expect(await screen.findByRole('button', { name: 'Platz annehmen' })).toBeInTheDocument();
      expect(
        screen.getByText('Ein Platz ist für Lina reserviert bis Fr., 23. Okt., 22:00.'),
      ).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage('en');
    }
  });
});
