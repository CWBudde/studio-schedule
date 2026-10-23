import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { catalogFixtures, ids, mockApiGet } from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { ScheduleFilters } from './ScheduleFilters';

afterEach(() => {
  vi.restoreAllMocks();
});

async function choose(label: string, option: string) {
  const user = userEvent.setup();
  const select = screen.getByRole('combobox', { name: label });
  await waitFor(() => expect(select).toBeEnabled());
  await user.click(select);
  await user.click(await screen.findByRole('option', { name: option }));
}

describe('ScheduleFilters', () => {
  it('writes each filter into the URL', async () => {
    mockApiGet(catalogFixtures);
    const { router } = renderAtSchedule(<ScheduleFilters />, { view: 'week' });
    await screen.findByRole('combobox', { name: 'Location' });

    await choose('Location', 'South Studio');
    await choose('Instructor', 'Riley Sample');
    await choose('Course type', 'Jazz');
    await choose('Level', 'Advanced');

    expect(router.state.location.search).toEqual({
      view: 'week',
      location: ids.locationSouth,
      instructor: ids.staffRiley,
      courseType: ids.jazz,
      level: 'advanced',
    });
  });

  it('clears a filter with its "all" option', async () => {
    mockApiGet(catalogFixtures);
    const { router } = renderAtSchedule(<ScheduleFilters />, {
      location: ids.locationNorth,
      level: 'open',
    });

    await screen.findByRole('combobox', { name: 'Location' });
    await waitFor(() =>
      expect(screen.getByRole('combobox', { name: 'Location' })).toHaveTextContent('North Studio'),
    );
    expect(screen.getByRole('combobox', { name: 'Level' })).toHaveTextContent('All levels');

    await choose('Location', 'All locations');
    await choose('Level', 'Any level');

    expect(router.state.location.search).toEqual({});
  });

  it('shows a reset button only while a filter is active', async () => {
    mockApiGet(catalogFixtures);
    const user = userEvent.setup();
    const { router } = renderAtSchedule(<ScheduleFilters />, {
      view: 'list',
      instructor: ids.staffAlex,
      courseType: ids.ballet,
    });

    await user.click(await screen.findByRole('button', { name: 'Reset filters' }));

    expect(router.state.location.search).toEqual({ view: 'list' });
    expect(screen.queryByRole('button', { name: 'Reset filters' })).not.toBeInTheDocument();
  });

  it('disables a select whose catalogue failed to load', async () => {
    mockApiGet({
      ...catalogFixtures,
      '/api/v1/locations': { status: 500, body: { message: 'boom' } },
    });
    renderAtSchedule(<ScheduleFilters />);

    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Instructor' })).toBeEnabled());
    expect(screen.getByRole('combobox', { name: 'Location' })).toBeDisabled();
    expect(screen.getByRole('combobox', { name: 'Level' })).toBeEnabled();
  });

  it('renders in German', async () => {
    mockApiGet(catalogFixtures);
    await i18n.changeLanguage('de');
    renderAtSchedule(<ScheduleFilters />);

    expect(await screen.findByRole('combobox', { name: 'Standort' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Stufe' })).toHaveTextContent('Jede Stufe');
  });
});
