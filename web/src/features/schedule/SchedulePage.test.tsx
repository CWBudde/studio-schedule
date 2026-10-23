import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ScheduleParams } from '@/api/queries';
import {
  catalogFixtures,
  ids,
  lessonBalletDetail,
  mockApiGet,
  type MockRequest,
  publicSettings,
  schedule,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { SchedulePage } from './SchedulePage';

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

/** Mocks the API and records the query of every schedule request. */
function mockSchedule() {
  const requests: ScheduleParams[] = [];
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/schedule': (init: MockRequest) => {
      requests.push(init.params?.query as ScheduleParams);
      return { body: schedule };
    },
    '/api/v1/lessons/{lessonId}': { body: lessonBalletDetail },
  });
  return requests;
}

describe('SchedulePage', () => {
  it('lists 14 days from the given date with countdown, filters and view switch', async () => {
    const requests = mockSchedule();

    renderAtSchedule(<SchedulePage />, { from: '2026-10-26' });

    expect(await screen.findByRole('heading', { name: 'Monday, October 26' })).toBeInTheDocument();
    expect(requests.at(-1)).toEqual({ from: '2026-10-26', to: '2026-11-09' });
    expect(screen.getByRole('heading', { level: 1, name: 'Schedule' })).toBeInTheDocument();
    expect(screen.getByRole('timer')).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Location' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'List' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByText('Oct 26 – Nov 8')).toBeInTheDocument();
    // Regular classes are released through Sunday, November 1 (releasedUntil
    // is the following school midnight); the list goes further.
    expect(
      screen.getByText(
        /Classes are published through Sunday, November 1\. The next ones follow on Sun, Oct 25, 18:00/,
      ),
    ).toBeInTheDocument();
  });

  it('does not mention the release window when the range is released', async () => {
    mockSchedule();

    renderAtSchedule(<SchedulePage />, { from: '2026-10-19', view: 'week' });

    expect(await screen.findByRole('region', { name: /Mon 19/ })).toBeInTheDocument();
    expect(screen.queryByText(/Classes are published through/)).not.toBeInTheDocument();
  });

  it('switches to the week of the date and asks for that week only', async () => {
    const requests = mockSchedule();
    const user = userEvent.setup();
    const { router } = renderAtSchedule(<SchedulePage />, { from: '2026-10-28' });

    await user.click(await screen.findByRole('radio', { name: 'Week' }));

    expect(router.state.location.search).toEqual({ from: '2026-10-28', view: 'week' });
    expect(await screen.findByRole('region', { name: /Mon 26/ })).toBeInTheDocument();
    expect(requests.at(-1)).toEqual({ from: '2026-10-26', to: '2026-11-02' });
  });

  it('pages through the list and passes the filters to the API', async () => {
    const requests = mockSchedule();
    const user = userEvent.setup();
    const { router } = renderAtSchedule(<SchedulePage />, {
      from: '2026-10-26',
      level: 'beginner',
      location: ids.locationNorth,
    });

    await user.click(await screen.findByRole('button', { name: 'Next 14 days' }));

    expect(router.state.location.search.from).toBe('2026-11-09');
    expect(requests.at(-1)).toEqual({
      from: '2026-11-09',
      to: '2026-11-23',
      level: 'beginner',
      location: ids.locationNorth,
    });
  });

  it('opens the detail sheet of a deep-linked lesson', async () => {
    mockSchedule();

    renderAtSchedule(<SchedulePage />, { from: '2026-10-26', lesson: ids.lessonBallet });

    const sheet = await screen.findByRole('dialog');
    expect(await within(sheet).findByText('1 Example Street, Sampletown')).toBeInTheDocument();
  });

  it('shows an error with retry when the schedule cannot be loaded', async () => {
    mockApiGet({
      ...catalogFixtures,
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { status: 500, body: { title: 'Internal Server Error' } },
    });

    renderAtSchedule(<SchedulePage />);

    expect(await screen.findByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});
