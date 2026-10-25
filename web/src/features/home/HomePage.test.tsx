import { screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  healthOk,
  ids,
  meMember,
  meOffice,
  mockApiGet,
  publicSettings,
  schedule,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { HomePage } from './HomePage';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('HomePage', () => {
  it('shows an OK badge and public settings when the backend is healthy', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
    });

    renderInRouter(<HomePage />);

    const status = within(await screen.findByRole('region', { name: 'System status' }));

    expect(await status.findByText('OK')).toBeInTheDocument();
    expect(status.getByText('Europe/Berlin')).toBeInTheDocument();
    // The version is no public information (ASVS 14.3.3).
    expect(status.queryByText('Version')).not.toBeInTheDocument();
    expect(status.getByText('Example Dance Studio')).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { level: 1, name: 'Example Dance Studio' }),
    ).toBeInTheDocument();
  });

  it.each([
    ['the office', { ...meOffice, version: '0.1.0-test' }, true],
    ['a member', meMember, false],
  ])('shows the version to %s only if the API names it', async (_, me, shown) => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
      '/api/v1/auth/me': { body: me },
    });

    renderInRouter(<HomePage />);

    const status = within(await screen.findByRole('region', { name: 'System status' }));
    expect(await status.findByText('Europe/Berlin')).toBeInTheDocument();
    if (shown) expect(await status.findByText('0.1.0-test')).toBeInTheDocument();
    else expect(status.queryByText('Version')).not.toBeInTheDocument();
  });

  it('teases the next lessons and links them into the schedule', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
    });

    renderInRouter(<HomePage />);

    const teaser = within(await screen.findByRole('region', { name: 'Next lessons' }));
    const ballet = await teaser.findByRole('link', { name: /Ballet/ });
    // Times are in the school time zone (Berlin), not the browser's.
    expect(ballet).toHaveTextContent('Mon, Oct 26, 18:00');
    expect(ballet).toHaveAttribute('href', `/schedule?from=2026-10-26&lesson=${ids.lessonBallet}`);
    expect(teaser.getByRole('link', { name: /Jazz/ })).toBeInTheDocument();
    // Canceled lessons are left out of the teaser.
    expect(teaser.queryByText('Contemporary')).not.toBeInTheDocument();
    expect(teaser.getByRole('link', { name: /Full schedule/ })).toHaveAttribute(
      'href',
      '/schedule',
    );
  });

  it('says so when no lessons are coming up', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: { ...schedule, lessons: [] } },
    });

    renderInRouter(<HomePage />);

    expect(await screen.findByText('No lessons in the next 7 days.')).toBeInTheDocument();
  });

  it('shows an error badge when the health check fails', async () => {
    mockApiGet({
      '/api/v1/health': { status: 503, body: { message: 'down' } },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
    });

    renderInRouter(<HomePage />);

    expect(await screen.findByText('Error')).toBeInTheDocument();
    expect(screen.getByText('Could not load data from the backend.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});
