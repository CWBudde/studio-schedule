import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { CalendarFeed } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  ids,
  meMember,
  mockApiDelete,
  mockApiGet,
  type MockRequest,
  publicSettings,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { CalendarFeedsCard } from './CalendarFeedsCard';

afterEach(() => {
  vi.restoreAllMocks();
});

const feeds: CalendarFeed[] = [
  {
    id: '0190a000-0000-7000-8000-000000000601',
    scope: 'bookings',
    createdAt: '2026-10-20T10:00:00Z',
    lastUsedAt: '2026-10-24T05:00:00Z',
  },
  {
    id: '0190a000-0000-7000-8000-000000000603',
    scope: 'schedule',
    locationId: ids.locationNorth,
    locationName: 'North Studio',
    // 23:30 UTC is already the next day in Berlin.
    createdAt: '2026-10-21T23:30:00Z',
  },
];

function mockFeeds(items: CalendarFeed[]) {
  let current = items;
  mockApiGet({
    '/api/v1/auth/me': { body: meMember },
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/my/calendar-feeds': () => ({ body: { items: current } }),
  });
  return mockApiDelete({
    '/api/v1/my/calendar-feeds/{feedId}': (init: MockRequest) => {
      current = current.filter((f) => f.id !== init.params?.path?.feedId);
      return { status: 204 };
    },
  });
}

function renderCard() {
  render(<Toaster />);
  renderInRouter(<CalendarFeedsCard />, { at: '/account' });
  return screen.findByRole('region', { name: 'Calendar feeds' });
}

describe('CalendarFeedsCard', () => {
  it('lists the active feeds with their subject and dates', async () => {
    mockFeeds(feeds);

    const card = within(await renderCard());

    const items = await card.findAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByText('Your lessons')).toBeInTheDocument();
    expect(
      await within(items[0]!).findByText(
        'Created on October 20, 2026 · last fetched on October 24, 2026',
      ),
    ).toBeInTheDocument();
    expect(within(items[1]!).getByText('Schedule, North Studio')).toBeInTheDocument();
    expect(
      within(items[1]!).getByText('Created on October 22, 2026 · not fetched yet'),
    ).toBeInTheDocument();
  });

  it('says when there is none', async () => {
    mockFeeds([]);

    const card = within(await renderCard());

    expect(await card.findByText(/You have no calendar links/)).toBeInTheDocument();
  });

  it('revokes a feed after asking', async () => {
    const revoke = mockFeeds(feeds);
    const user = userEvent.setup();

    const card = within(await renderCard());
    await user.click(
      await card.findByRole('button', { name: 'Revoke the link of Schedule, North Studio' }),
    );
    const confirm = within(card.getByRole('group', { name: 'Revoke this link?' }));
    await user.click(confirm.getByRole('button', { name: 'Revoke' }));

    expect(revoke).toHaveBeenCalledWith('/api/v1/my/calendar-feeds/{feedId}', {
      params: { path: { feedId: feeds[1]!.id } },
    });
    expect(await screen.findByText('The link has been revoked.')).toBeInTheDocument();
    await expect.poll(() => card.queryAllByRole('listitem')).toHaveLength(1);
    expect(card.queryByText('Schedule, North Studio')).not.toBeInTheDocument();
  });

  it('speaks German', async () => {
    await i18n.changeLanguage('de');
    mockFeeds(feeds);

    render(<Toaster />);
    renderInRouter(<CalendarFeedsCard />, { at: '/account' });
    const card = within(await screen.findByRole('region', { name: 'Kalender-Abos' }));

    expect(await card.findByText('Deine Stunden')).toBeInTheDocument();
    expect(
      await card.findByText('Erstellt am 20. Oktober 2026 · zuletzt abgerufen am 24. Oktober 2026'),
    ).toBeInTheDocument();
    expect(
      card.getByRole('button', { name: 'Den Link für Stundenplan, North Studio widerrufen' }),
    ).toBeInTheDocument();
  });
});
