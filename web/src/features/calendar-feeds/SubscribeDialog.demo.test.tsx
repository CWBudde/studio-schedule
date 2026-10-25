import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { openDownload } from '@/api/download';
import { meMember, mockApiGet, mockApiPost, publicSettings } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { SubscribeButton } from './SubscribeDialog';

// The web demo: the backend runs in the browser, so no calendar app can
// reach a feed, and downloads go through the worker bridge.
vi.mock('@/lib/env', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/env')>()),
  transport: 'wasm',
  isDemo: true,
}));
vi.mock('@/api/download', () => ({ openDownload: vi.fn(() => Promise.resolve()) }));

afterEach(() => {
  vi.restoreAllMocks();
});

describe('SubscribeButton in the web demo', () => {
  it('offers only the download of a new link', async () => {
    mockApiGet({
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/my/calendar-feeds': { body: { items: [] } },
    });
    mockApiPost({
      '/api/v1/my/calendar-feeds': {
        status: 201,
        body: {
          feed: {
            id: '0190a000-0000-7000-8000-000000000602',
            scope: 'bookings',
            createdAt: '2026-10-25T09:00:00Z',
          },
          token: 'tok-secret',
          path: '/ical/tok-secret.ics',
        },
      },
    });
    const user = userEvent.setup();

    renderInRouter(<SubscribeButton subject={{ scope: 'bookings' }} />, { at: '/my-lessons' });
    await user.click(await screen.findByRole('button', { name: 'Subscribe' }));
    const dialog = within(await screen.findByRole('dialog'));
    await user.click(await dialog.findByRole('button', { name: 'Create link' }));

    const created = within(await dialog.findByRole('region', { name: 'Your new link' }));
    expect(created.getByText(/calendar apps cannot reach this link/)).toBeInTheDocument();
    expect(created.queryByRole('textbox')).not.toBeInTheDocument();
    expect(created.queryByRole('button', { name: 'Copy link' })).not.toBeInTheDocument();
    expect(created.queryByRole('link', { name: 'Open in calendar app' })).not.toBeInTheDocument();

    await user.click(created.getByRole('button', { name: 'Download .ics' }));
    expect(openDownload).toHaveBeenCalledWith('/ical/tok-secret.ics?download=true');
  });
});
