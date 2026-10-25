import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { MyAnnouncement } from '@/api/queries';
import i18n from '@/i18n';
import {
  meImpersonated,
  meMember,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  signedOut,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { AnnouncementBanner } from './AnnouncementBanner';
import { AnnouncementList } from './AnnouncementList';

afterEach(() => {
  vi.restoreAllMocks();
});

/** Fictional announcements as GET /announcements lists them (newest first). */
const myAnnouncements: MyAnnouncement[] = [
  {
    id: '0190a000-0000-7000-8000-000000000901',
    title: 'Costume fitting',
    body: 'Please come on Saturday.\nIt takes twenty minutes.',
    publishedAt: '2026-10-21T07:00:00Z',
    dismissed: false,
  },
  {
    id: '0190a000-0000-7000-8000-000000000902',
    title: 'Studio closed on Monday',
    body: 'No lessons on Monday.',
    publishedAt: '2026-10-20T07:00:00Z',
    expiresAt: '2026-10-27T07:00:00Z',
    dismissed: false,
  },
  {
    id: '0190a000-0000-7000-8000-000000000903',
    title: 'Welcome',
    body: 'Book your lessons here.',
    publishedAt: '2026-10-18T07:00:00Z',
    dismissed: true,
  },
];

const signedIn = (items: MyAnnouncement[] = myAnnouncements, me = meMember) => ({
  '/api/v1/auth/me': { body: me },
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/announcements': { body: { items } },
});

const banner = () => screen.findByRole('region', { name: 'New announcements' });

describe('AnnouncementBanner', () => {
  it('shows the newest announcement not dismissed, and how many more there are', async () => {
    mockApiGet(signedIn());

    renderInRouter(<AnnouncementBanner />);

    const region = within(await banner());
    expect(region.getByText('Costume fitting')).toBeInTheDocument();
    expect(region.getByText('and 1 more')).toBeInTheDocument();
    // The dismissed one and the texts stay hidden until it is opened.
    expect(region.queryByText('Welcome')).toBeNull();
    expect(region.queryByText(/Please come on Saturday/)).toBeNull();
  });

  it('opens all of them with their text, in the school time zone', async () => {
    mockApiGet(signedIn());
    const user = userEvent.setup();

    renderInRouter(<AnnouncementBanner />);
    const region = within(await banner());
    await user.click(region.getByRole('button', { name: 'Show the 2 announcements' }));

    const items = region.getAllByRole('listitem');
    expect(items.map((li) => li.querySelector('p')?.textContent)).toEqual([
      'Costume fitting',
      'Studio closed on Monday',
    ]);
    expect(region.getByText(/Please come on Saturday\.\s+It takes twenty minutes\./)).toHaveClass(
      'whitespace-pre-line',
    );
    // Berlin time, though the tests run in New York.
    expect(region.getByText('Published Oct 21, 2026, 09:00')).toBeInTheDocument();
    expect(region.getByRole('button', { name: 'Hide' })).toHaveAttribute('aria-expanded', 'true');
  });

  it('dismisses an announcement', async () => {
    mockApiGet(signedIn());
    const post = mockApiPost({
      '/api/v1/announcements/{announcementId}/dismiss': { status: 204 },
    });
    const user = userEvent.setup();

    renderInRouter(<AnnouncementBanner />);
    const region = within(await banner());
    await user.click(region.getByRole('button', { name: 'Dismiss “Costume fitting”' }));

    expect(post).toHaveBeenCalledWith('/api/v1/announcements/{announcementId}/dismiss', {
      params: { path: { announcementId: myAnnouncements[0]?.id } },
    });
    expect(await region.findByText('Studio closed on Monday')).toBeInTheDocument();
    expect(region.queryByText('Costume fitting')).toBeNull();
    expect(region.queryByText('and 1 more')).toBeNull();
  });

  it('hides a dismissed one for the visit while an admin is signed in as the account', async () => {
    // The API stores nothing then and lists it as not dismissed.
    mockApiGet(signedIn([myAnnouncements[1] as MyAnnouncement], meImpersonated));
    mockApiPost({ '/api/v1/announcements/{announcementId}/dismiss': { status: 204 } });
    const user = userEvent.setup();

    renderInRouter(<AnnouncementBanner />);
    await user.click(
      within(await banner()).getByRole('button', { name: 'Dismiss “Studio closed on Monday”' }),
    );

    await vi.waitFor(() =>
      expect(screen.queryByRole('region', { name: 'New announcements' })).toBeNull(),
    );
  });

  it('says when the dismissal failed', async () => {
    mockApiGet(signedIn());
    mockApiPost({
      '/api/v1/announcements/{announcementId}/dismiss': problem(503, 'unavailable'),
    });
    const user = userEvent.setup();

    renderInRouter(<AnnouncementBanner />);
    await user.click(
      within(await banner()).getByRole('button', { name: 'Dismiss “Costume fitting”' }),
    );

    expect(await screen.findByRole('region', { name: 'New announcements' })).toHaveTextContent(
      'Costume fitting',
    );
  });

  it.each([
    ['visitors', signedOut],
    [
      'an account whose announcements are all dismissed',
      signedIn([myAnnouncements[2] as MyAnnouncement]),
    ],
    ['an account without announcements', signedIn([])],
  ])('shows nothing to %s', async (_, fixtures) => {
    const get = mockApiGet(fixtures);

    renderInRouter(<AnnouncementBanner />);

    await vi.waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/auth/me', expect.anything()));
    expect(screen.queryByRole('region', { name: 'New announcements' })).toBeNull();
    if (fixtures === signedOut) {
      expect(get).not.toHaveBeenCalledWith('/api/v1/announcements', expect.anything());
    }
  });

  it('renders in German', async () => {
    mockApiGet(signedIn());
    await i18n.changeLanguage('de');

    renderInRouter(<AnnouncementBanner />);

    const region = within(await screen.findByRole('region', { name: 'Neue Mitteilungen' }));
    expect(region.getByText('und 1 weitere')).toBeInTheDocument();
    expect(
      region.getByRole('button', { name: '„Costume fitting“ ausblenden' }),
    ).toBeInTheDocument();
  });
});

describe('AnnouncementList', () => {
  const list = () => screen.findByRole('region', { name: 'Announcements' });

  it('lists every announcement, newest first, the dismissed ones marked', async () => {
    mockApiGet(signedIn());

    renderInRouter(<AnnouncementList />);

    const items = await within(await list()).findAllByRole('listitem');
    expect(items.map((li) => li.querySelector('p')?.textContent)).toEqual([
      'Costume fitting',
      'Studio closed on Monday',
      'Welcome',
    ]);
    expect(within(items[2] as HTMLElement).getByText('Dismissed')).toBeInTheDocument();
    expect(within(items[0] as HTMLElement).queryByText('Dismissed')).toBeNull();
    expect(within(items[1] as HTMLElement).getByText('No lessons on Monday.')).toBeInTheDocument();
  });

  it('says when there are none', async () => {
    mockApiGet(signedIn([]));

    renderInRouter(<AnnouncementList />);

    expect(await screen.findByText('There are no announcements right now.')).toBeInTheDocument();
  });

  it('says when they cannot be loaded, and loads them again', async () => {
    let fail = true;
    mockApiGet({
      ...signedIn(),
      '/api/v1/announcements': () =>
        fail ? problem(503, 'unavailable') : { body: { items: myAnnouncements } },
    });
    const user = userEvent.setup();

    renderInRouter(<AnnouncementList />);
    expect(await screen.findByText('The announcements could not be loaded.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByText('Costume fitting')).toBeInTheDocument();
  });

  it('shows nothing to visitors', async () => {
    const get = mockApiGet(signedOut);

    renderInRouter(<AnnouncementList />);

    await vi.waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/auth/me', expect.anything()));
    expect(screen.queryByRole('region', { name: 'Announcements' })).toBeNull();
  });
});
