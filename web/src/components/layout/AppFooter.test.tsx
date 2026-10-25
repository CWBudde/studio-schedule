import { screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { meMember, meOffice, mockApiGet, publicSettings } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { AppFooter } from './AppFooter';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('AppFooter', () => {
  it('names the version for the office, for support', async () => {
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/auth/me': { body: { ...meOffice, version: '0.1.0-test' } },
    });

    renderInRouter(<AppFooter />);

    expect(await screen.findByText('Studio Schedule · Version 0.1.0-test')).toBeInTheDocument();
  });

  it.each([
    ['a member', { body: meMember }],
    ['a visitor', { status: 401, body: { title: 'Unauthorized', status: 401 } }],
  ])('names no version for %s (ASVS 14.3.3)', async (_, me) => {
    const get = mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/auth/me': me,
    });

    renderInRouter(<AppFooter />);

    expect(await screen.findByRole('link', { name: 'Source code on GitHub' })).toBeInTheDocument();
    await vi.waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/auth/me', expect.anything()));
    expect(screen.queryByText(/Version/)).not.toBeInTheDocument();
    expect(screen.getByText('Studio Schedule')).toBeInTheDocument();
  });
});
