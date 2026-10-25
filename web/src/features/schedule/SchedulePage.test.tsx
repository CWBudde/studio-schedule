import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me, Schedule } from '@/api/client';
import type { ScheduleParams } from '@/api/queries';
import {
  catalogFixtures,
  ids,
  lessonBalletDetail,
  lessonWorkshopOffice,
  meMember,
  meOffice,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  publicSettings,
  schedule,
  staffEntries,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { SchedulePage } from './SchedulePage';

// Friday 2026-10-23, before the fixture lessons: no day is today or
// tomorrow and no lesson has ended.
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-23T10:00:00Z'));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

/** Mocks the API and records the query of every schedule request; `me` is signed in. */
function mockSchedule(answer: Schedule = schedule, me?: Me) {
  const requests: ScheduleParams[] = [];
  mockApiGet({
    ...catalogFixtures,
    ...(me ? { '/api/v1/auth/me': { body: me } } : {}),
    '/api/v1/admin/staff': { body: { items: staffEntries } },
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/schedule': (init: MockRequest) => {
      requests.push(init.params?.query as ScheduleParams);
      return { body: answer };
    },
    '/api/v1/lessons/{lessonId}': { body: lessonBalletDetail },
    '/api/v1/my/calendar-feeds': { body: { items: [] } },
  });
  return requests;
}

describe('SchedulePage', () => {
  it('lists 14 days from the given date with countdown, filters and view switch', async () => {
    const requests = mockSchedule();

    renderAtSchedule(<SchedulePage />, { from: '2026-10-26' });

    expect(await screen.findByRole('heading', { name: 'Monday, October 26' })).toBeInTheDocument();
    expect(requests).toContainEqual({ from: '2026-10-26', to: '2026-11-09' });
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

  it('tells a member with early access up to which day they see classes', async () => {
    // Early access through Friday, November 6; released through November 1.
    mockSchedule({ ...schedule, visibleUntil: '2026-11-06T23:00:00Z' });

    renderAtSchedule(<SchedulePage />, { from: '2026-10-26' });

    expect(
      await screen.findByText(
        'Early access: as a member you already see classes through Friday, November 6. For everyone, they are published through Sunday, November 1.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/^Classes are published through/)).not.toBeInTheDocument();
  });

  it('treats a member without early access like a guest', async () => {
    mockSchedule({ ...schedule, visibleUntil: schedule.releasedUntil });

    renderAtSchedule(<SchedulePage />, { from: '2026-10-26' });

    expect(await screen.findByText(/^Classes are published through/)).toBeInTheDocument();
    expect(screen.queryByText(/Early access/)).not.toBeInTheDocument();
  });

  it('tells staff that they see every lesson', async () => {
    // Staff get no visibleUntil: they see hidden and unreleased lessons too.
    const staffSchedule: Schedule = { ...schedule };
    delete staffSchedule.visibleUntil;
    mockSchedule(staffSchedule);

    renderAtSchedule(<SchedulePage />, { from: '2026-10-19', view: 'week' });

    expect(
      await screen.findByText(
        'As staff you see every lesson, including hidden ones and classes that are not published yet. For everyone, classes are published through Sunday, November 1.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText(/^Classes are published through/)).not.toBeInTheDocument();
  });

  it('switches to the week of the date and asks for that week only', async () => {
    const requests = mockSchedule();
    const user = userEvent.setup();
    const { router } = renderAtSchedule(<SchedulePage />, { from: '2026-10-28' });

    await user.click(await screen.findByRole('radio', { name: 'Week' }));

    expect(router.state.location.search).toEqual({ from: '2026-10-28', view: 'week' });
    expect(await screen.findByRole('region', { name: /Mon 26/ })).toBeInTheDocument();
    expect(requests).toContainEqual({ from: '2026-10-26', to: '2026-11-02' });
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
    expect(requests).toContainEqual({
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

  it('lets the office add a one-off lesson and opens it', async () => {
    mockSchedule(schedule, meOffice);
    const post = mockApiPost({ '/api/v1/lessons': { status: 201, body: lessonWorkshopOffice } });
    const user = userEvent.setup();
    const { router } = renderAtSchedule(<SchedulePage />, { from: '2026-10-26' });

    await user.click(await screen.findByRole('button', { name: 'New lesson' }));

    const sheet = within(await screen.findByRole('dialog', { name: 'New one-off lesson' }));
    await user.click(await sheet.findByRole('combobox', { name: 'Course type' }));
    await user.click(await screen.findByRole('option', { name: 'Jazz' }));
    await user.type(sheet.getByLabelText('Date'), '2026-11-07');
    await user.type(sheet.getByLabelText('Start time'), '14:00');
    await user.type(sheet.getByLabelText('Duration (minutes)'), '180');
    await user.click(sheet.getByRole('combobox', { name: 'Room' }));
    await user.click(await screen.findByRole('option', { name: 'North Studio · Room A' }));
    await user.click(sheet.getByRole('combobox', { name: 'Instructor' }));
    await user.click(await screen.findByRole('option', { name: 'Riley Sample' }));
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));

    await waitFor(() => expect(router.state.location.search.lesson).toBe(ids.lessonWorkshop));
    expect(post).toHaveBeenCalledTimes(1);
  });

  it('offers no new lesson to a member', async () => {
    mockSchedule(schedule, meMember);

    renderAtSchedule(<SchedulePage />, { from: '2026-10-26' });

    expect(await screen.findByRole('heading', { name: 'Monday, October 26' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'New lesson' })).not.toBeInTheDocument();
  });

  it('subscribes to the whole schedule when the filter names an unknown location', async () => {
    mockSchedule(schedule, meMember);
    const user = userEvent.setup();

    renderAtSchedule(<SchedulePage />, {
      from: '2026-10-26',
      location: '0190a000-0000-7000-8000-000000000999',
    });
    await user.click(await screen.findByRole('button', { name: 'Subscribe' }));

    expect(
      await screen.findByRole('dialog', { name: 'The schedule in your calendar' }),
    ).toBeInTheDocument();
  });

  it('subscribes to the schedule of the location the filter selects', async () => {
    mockSchedule(schedule, meMember);
    const post = mockApiPost({
      '/api/v1/my/calendar-feeds': {
        status: 201,
        body: {
          feed: {
            id: '0190a000-0000-7000-8000-000000000603',
            scope: 'schedule',
            locationId: ids.locationNorth,
            locationName: 'North Studio',
            createdAt: '2026-10-23T10:00:00Z',
          },
          token: 'tok',
          path: '/ical/tok.ics',
        },
      },
    });
    const user = userEvent.setup();

    renderAtSchedule(<SchedulePage />, { from: '2026-10-26', location: ids.locationNorth });
    await user.click(await screen.findByRole('button', { name: 'Subscribe' }));
    const dialog = within(
      await screen.findByRole('dialog', { name: 'North Studio in your calendar' }),
    );
    await user.click(await dialog.findByRole('button', { name: 'Create link' }));

    expect(post).toHaveBeenCalledWith('/api/v1/my/calendar-feeds', {
      body: { scope: 'schedule', locationId: ids.locationNorth },
    });
    expect(await dialog.findByRole('region', { name: 'Your new link' })).toBeInTheDocument();
  });
});
