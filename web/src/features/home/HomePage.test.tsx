import { screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { healthOk, mockApiGet, publicSettings } from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { HomePage } from './HomePage';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('HomePage', () => {
  it('shows an OK badge, version and public settings when the backend is healthy', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
    });

    renderWithQuery(<HomePage />);

    const card = screen
      .getByRole('heading', { name: 'System status' })
      .closest('[data-slot="card"]');
    expect(card).not.toBeNull();
    const status = within(card as HTMLElement);

    expect(await status.findByText('OK')).toBeInTheDocument();
    expect(status.getByText('0.1.0-test')).toBeInTheDocument();
    expect(status.getByText('Europe/Berlin')).toBeInTheDocument();
    expect(status.getByText('Example Dance Studio')).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { level: 1, name: 'Example Dance Studio' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Schedule coming in Phase 2')).toBeInTheDocument();
  });

  it('shows an error badge when the health check fails', async () => {
    mockApiGet({
      '/api/v1/health': { status: 503, body: { message: 'down' } },
      '/api/v1/settings/public': { body: publicSettings },
    });

    renderWithQuery(<HomePage />);

    expect(await screen.findByText('Error')).toBeInTheDocument();
    expect(screen.getByText('Could not load data from the backend.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});
