import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Statistics } from '@/api/queries';
import i18n from '@/i18n';
import { type MockRequest, mockApiGet } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { StatisticsPage } from './StatisticsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const figures = {
  lessons: 3,
  capacity: 19,
  bookings: 5,
  utilization: 5 / 19,
  attended: 2,
  noShows: 2,
  noShowRate: 0.5,
  lateCancellations: 1,
  trials: 1,
  guests: 1,
  waitlistEntries: 1,
};

/**
 * The figures of 2026-10-19 to 2026-10-26 in Europe/Berlin: two weeks, the
 * second one day long, around the end of daylight saving time.
 */
const stats: Statistics = {
  timezone: 'Europe/Berlin',
  currency: 'EUR',
  today: '2026-10-27',
  range: 'last4Weeks',
  from: '2026-10-19',
  to: '2026-10-26',
  granularity: 'week',
  totals: {
    ...figures,
    passesSold: 1,
    salesGross: 4500,
    canceledLessons: 1,
    activeMembers: 3,
  },
  periods: [
    {
      start: '2026-10-19',
      end: '2026-10-25',
      ...figures,
      lessons: 2,
      bookings: 3,
      passesSold: 1,
      salesGross: 9000,
    },
    {
      start: '2026-10-26',
      end: '2026-10-26',
      ...figures,
      lessons: 1,
      bookings: 2,
      noShowRate: undefined,
      attended: 0,
      noShows: 0,
      passesSold: 0,
      salesGross: -4500,
    },
  ],
  classes: [
    {
      classId: '00000000-0000-7000-8000-000000000001',
      courseTypeName: 'Imaginary Ballet',
      level: 'beginner',
      weekday: 1,
      startTime: '16:30',
      ...figures,
    },
    {
      lessonId: '00000000-0000-7000-8000-000000000002',
      courseTypeName: 'Imaginary Tap',
      // Sunday 23:30 in Berlin, the first hour of CET; still Sunday in New York.
      startsAt: '2026-10-25T22:30:00Z',
      ...figures,
      noShowRate: undefined,
    },
  ],
  rooms: [
    {
      roomId: '00000000-0000-7000-8000-000000000003',
      name: 'Room One',
      locationName: 'Studio Nowhere',
      ...figures,
    },
  ],
  courseTypes: [
    {
      courseTypeId: '00000000-0000-7000-8000-000000000004',
      name: 'Imaginary Ballet',
      ...figures,
    },
  ],
  instructors: [
    { staffId: '00000000-0000-7000-8000-000000000005', name: 'Ada Example', ...figures },
  ],
  topParticipants: [
    {
      personId: '00000000-0000-7000-8000-000000000006',
      givenName: 'Fia',
      familyName: 'Fictional',
      bookings: 2,
      attended: 1,
      lateCancellations: 0,
    },
    {
      personId: '00000000-0000-7000-8000-000000000007',
      givenName: 'Noa',
      familyName: 'Nobody',
      bookings: 1,
      attended: 0,
      lateCancellations: 3,
    },
  ],
};

/** Answers the statistics; returns the query params of each request. */
function mockStatistics(
  answer: (q: Record<string, unknown>) => { status?: number; body?: unknown },
) {
  const asked: Record<string, unknown>[] = [];
  mockApiGet({
    '/api/v1/admin/statistics': (init: MockRequest) => {
      const q = init.params?.query ?? {};
      asked.push(q);
      return answer(q);
    },
  });
  return asked;
}

describe('StatisticsPage', () => {
  it('shows the totals, the periods and the groups of the last four weeks', async () => {
    const asked = mockStatistics(() => ({ body: stats }));

    renderInRouter(<StatisticsPage />, { at: '/admin/statistics' });

    const tiles = await screen.findByRole('region', { name: 'Totals of the range' });
    expect(asked[0]).toEqual({ range: 'last4Weeks' });
    expect(screen.getByRole('radio', { name: 'Last 4 weeks' })).toBeChecked();
    expect(screen.getByText('Oct 19, 2026 – Oct 26, 2026 · by week')).toBeInTheDocument();

    const tile = (label: string) => within(tiles).getByText(label).closest('div')!;
    expect(within(tile('Bookings')).getByText('5')).toBeInTheDocument();
    expect(within(tile('Bookings')).getByText('in 3 lessons')).toBeInTheDocument();
    expect(within(tile('Utilization')).getByText('26%')).toBeInTheDocument();
    expect(within(tile('No-show rate')).getByText('50%')).toBeInTheDocument();
    expect(
      within(tile('No-show rate')).getByText('2 of 4 with attendance taken'),
    ).toBeInTheDocument();
    expect(within(tile('Pass sales')).getByText('€45.00')).toBeInTheDocument();
    expect(within(tile('Pass sales')).getByText('1 pass sold')).toBeInTheDocument();
    expect(within(tile('Active members')).getByText('3')).toBeInTheDocument();
    expect(screen.getByText('1 canceled lesson is left out of every figure.')).toBeInTheDocument();

    // The charts name the table that holds their figures.
    expect(
      screen.getByRole('img', {
        name: 'Bookings by week. Column chart; the figures are in the table “By week”.',
      }),
    ).toBeInTheDocument();

    // The weeks in school dates, the last one cut to the range.
    const weeks = screen.getByRole('table', { name: 'By week' });
    const rows = within(weeks).getAllByRole('row');
    expect(rows).toHaveLength(3);
    expect(within(rows[1]!).getByRole('rowheader')).toHaveTextContent('Oct 19 – Oct 25');
    expect(within(rows[1]!).getByText('€90.00')).toBeInTheDocument();
    expect(within(rows[2]!).getByRole('rowheader')).toHaveTextContent('Oct 26');
    expect(within(rows[2]!).getByText('-€45.00')).toBeInTheDocument();

    // A weekly class by its weekday and time; a one-off lesson by its start
    // in the school's time zone, not the browser's.
    const classes = screen.getByRole('table', { name: 'By class' });
    expect(
      within(classes).getByRole('rowheader', { name: 'Imaginary Ballet · Mon 16:30' }),
    ).toBeInTheDocument();
    const oneOff = within(classes).getByRole('rowheader', {
      name: 'Imaginary Tap · Sun, Oct 25, 23:30',
    });
    expect(within(oneOff.closest('tr')!).getByText('–')).toBeInTheDocument();

    expect(
      within(screen.getByRole('table', { name: 'By room' })).getByRole('rowheader', {
        name: 'Room One · Studio Nowhere',
      }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole('table', { name: 'By instructor' })).getByRole('rowheader', {
        name: 'Ada Example',
      }),
    ).toBeInTheDocument();
  });

  it('ranks the top participants and links each to their page', async () => {
    mockStatistics(() => ({ body: stats }));

    renderInRouter(<StatisticsPage />, { at: '/admin/statistics' });

    const table = await screen.findByRole('table', { name: 'Top participants' });
    expect(
      screen.getByText(
        'Up to 20 persons with the most bookings in the range; canceled bookings do not count.',
      ),
    ).toBeInTheDocument();
    const rows = within(table).getAllByRole('row');
    expect(rows).toHaveLength(3);
    expect(
      within(rows[0]!)
        .getAllByRole('columnheader')
        .map((h) => h.textContent),
    ).toEqual(['Rank', 'Person', 'Bookings', 'Attended', 'Late cancellations']);
    expect(
      within(rows[1]!)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['1', '2', '1', '0']);
    expect(
      within(rows[2]!)
        .getAllByRole('cell')
        .map((c) => c.textContent),
    ).toEqual(['2', '1', '0', '3']);
    expect(within(rows[1]!).getByRole('link', { name: 'Fia Fictional' })).toHaveAttribute(
      'href',
      '/admin/persons/00000000-0000-7000-8000-000000000006',
    );
    expect(within(rows[2]!).getByRole('rowheader')).toHaveTextContent('Noa Nobody');
  });

  it('says so when nobody booked', async () => {
    mockStatistics(() => ({ body: { ...stats, topParticipants: [] } }));

    renderInRouter(<StatisticsPage />, { at: '/admin/statistics' });

    const heading = await screen.findByRole('heading', { name: 'Top participants' });
    expect(screen.queryByRole('table', { name: 'Top participants' })).not.toBeInTheDocument();
    expect(
      within(heading.closest('section')!).getByText('Nothing in this range.'),
    ).toBeInTheDocument();
  });

  it('switches to another preset through the URL', async () => {
    const asked = mockStatistics((q) => ({ body: { ...stats, range: q.range } }));
    const user = userEvent.setup();

    const { router } = renderInRouter(<StatisticsPage />, { at: '/admin/statistics' });
    await screen.findByRole('table', { name: 'By week' });

    await user.click(screen.getByRole('radio', { name: 'Last 12 weeks' }));

    await waitFor(() => expect(asked.at(-1)).toEqual({ range: 'last12Weeks' }));
    expect(router.state.location.search).toEqual({ range: 'last12Weeks' });
    expect(screen.getByRole('radio', { name: 'Last 12 weeks' })).toBeChecked();
  });

  it('asks for a custom range of at most a year', async () => {
    const asked = mockStatistics(() => ({ body: { ...stats, range: 'custom' } }));
    const user = userEvent.setup();

    const { router } = renderInRouter(<StatisticsPage />, { at: '/admin/statistics' });
    await screen.findByRole('table', { name: 'By week' });

    // A custom range starts as the range shown.
    await user.click(screen.getByRole('radio', { name: 'Custom' }));
    const from = screen.getByLabelText('From');
    const to = screen.getByLabelText('To');
    expect(from).toHaveValue('2026-10-19');
    expect(to).toHaveValue('2026-10-26');
    await waitFor(() =>
      expect(asked.at(-1)).toEqual({ range: 'custom', from: '2026-10-19', to: '2026-10-26' }),
    );

    // 367 days are too many: nothing is asked.
    const count = asked.length;
    await user.clear(from);
    await user.type(from, '2025-10-25');
    await user.click(screen.getByRole('button', { name: 'Show' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Choose at most 366 days.');
    expect(from).toHaveAccessibleDescription('Choose at most 366 days.');
    expect(asked).toHaveLength(count);

    await user.clear(from);
    await user.type(from, '2025-10-26');
    await user.click(screen.getByRole('button', { name: 'Show' }));
    await waitFor(() =>
      expect(asked.at(-1)).toEqual({ range: 'custom', from: '2025-10-26', to: '2026-10-26' }),
    );
    expect(router.state.location.search).toEqual({
      range: 'custom',
      from: '2025-10-26',
      to: '2026-10-26',
    });
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('takes the custom dates from the URL when it changes', async () => {
    mockStatistics(() => ({ body: { ...stats, range: 'custom' } }));
    const user = userEvent.setup();

    const { router } = renderInRouter(<StatisticsPage />, {
      at: '/admin/statistics',
      search: { range: 'custom', from: '2026-10-19', to: '2026-10-26' },
    });
    await screen.findByRole('table', { name: 'By week' });
    const from = screen.getByLabelText('From');
    await user.clear(from);
    await user.type(from, '2026-01-01');

    // Another range in the URL, e.g. a link, replaces the draft …
    await router.navigate({
      to: '/admin/statistics',
      search: { range: 'custom', from: '2026-09-01', to: '2026-09-30' },
    });
    await waitFor(() => expect(screen.getByLabelText('From')).toHaveValue('2026-09-01'));
    expect(screen.getByLabelText('To')).toHaveValue('2026-09-30');

    // … and so does going back.
    router.history.back();
    await waitFor(() => expect(screen.getByLabelText('From')).toHaveValue('2026-10-19'));
    expect(screen.getByLabelText('To')).toHaveValue('2026-10-26');
  });

  it('explains a term range without a current term', async () => {
    mockStatistics(() => ({ status: 404, body: { title: 'Not Found', status: 404 } }));

    renderInRouter(<StatisticsPage />, { at: '/admin/statistics', search: { range: 'term' } });

    expect(
      await screen.findByText('No term includes today. Choose another range.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Current term' })).toBeChecked();
  });

  it('speaks German', async () => {
    await i18n.changeLanguage('de');
    mockStatistics(() => ({ body: { ...stats, granularity: 'month' } }));

    renderInRouter(<StatisticsPage />, { at: '/admin/statistics' });

    expect(await screen.findByRole('table', { name: 'Nach Monaten' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Statistik' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Buchungen nach Monaten' })).toBeInTheDocument();
    expect(screen.getByText('in 3 Stunden')).toBeInTheDocument();
  });
});
