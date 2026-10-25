import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ScheduleLesson } from '@/api/client';
import type {
  Booking,
  MyLessonBooking,
  MyLessonHistory,
  MyLessons,
  MyWaitlistEntry,
} from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  family,
  ids,
  lessonBallet,
  lessonBalletDetail,
  lessonContemporary,
  lessonWorkshop,
  meMember,
  type MockRequest,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  publicSettings,
  signedOut,
} from '@/test/mockApi';
import { renderAtMyLessons } from '@/test/utils';
import { MyLessonsPage } from './MyLessonsPage';

// Sunday 2026-10-25 19:00 in Berlin: after the ballet lesson's late-cancel
// cutoff (the day before, 18:00), long before the workshop's.
const now = '2026-10-25T18:00:00Z';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(now));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;
const nora = { id: meMember.personId, name: 'Nora Example' };
const lina = { id: family.dependents[0]!.id, name: 'Lina Example' };
const max = { id: family.dependents[1]!.id, name: 'Max Example' };
const alone = { ...family, dependents: [] };

/** A lesson as "My lessons" summarises it. */
const summary = ({
  bookingClosesAt: _closes,
  lateCancelFrom: _late,
  freePlaces: _free,
  bookedFor: _booked,
  waitlist: _waitlist,
  ...rest
}: ScheduleLesson): MyLessonBooking['lesson'] => rest;

const booking = (
  n: number,
  person: { id: string },
  lesson: ScheduleLesson,
  extra: Partial<Booking> = {},
): Booking => ({
  id: id(600 + n),
  lessonId: lesson.id,
  personId: person.id,
  status: 'booked',
  source: 'self',
  bookedAt: '2026-10-20T10:00:00Z',
  late: false,
  ...extra,
});

const upcoming = (
  n: number,
  person: { id: string; name: string },
  lesson: ScheduleLesson,
): MyLessonBooking => ({
  booking: booking(n, person, lesson),
  personName: person.name,
  lesson: summary(lesson),
  ...(lesson.lateCancelFrom ? { lateCancelFrom: lesson.lateCancelFrom } : {}),
  canCancel: true,
});

/** A full lesson on Thursday 2026-10-29, 18:30–20:00 in Berlin, with the family waiting. */
const fullLesson: ScheduleLesson = {
  ...lessonBallet,
  id: id(104),
  startsAt: '2026-10-29T17:30:00Z',
  endsAt: '2026-10-29T19:00:00Z',
  bookingClosesAt: '2026-10-29T16:30:00Z',
  lateCancelFrom: '2026-10-28T17:30:00Z',
  freePlaces: 0,
};

const noraInvited: MyWaitlistEntry = {
  entry: {
    entryId: id(701),
    personId: nora.id,
    name: nora.name,
    status: 'invited',
    stayOnList: false,
    expiresAt: '2026-10-26T06:00:00Z',
  },
  lesson: summary(fullLesson),
  bookingClosesAt: fullLesson.bookingClosesAt,
};

const maxWaiting: MyWaitlistEntry = {
  entry: {
    entryId: id(702),
    personId: max.id,
    name: max.name,
    status: 'waiting',
    stayOnList: true,
    position: 2,
  },
  lesson: summary(fullLesson),
  bookingClosesAt: fullLesson.bookingClosesAt,
};

/**
 * Coming up: Nora and Lina in Monday's ballet class (late to cancel by now),
 * Lina in Tuesday's contemporary class, which the school canceled, and Max
 * in the workshop on November 7.
 */
const myLessons: MyLessons = {
  timezone: 'Europe/Berlin',
  now,
  upcoming: [
    upcoming(1, nora, lessonBallet),
    upcoming(2, lina, lessonBallet),
    upcoming(3, lina, lessonContemporary),
    upcoming(4, max, lessonWorkshop),
  ],
  waitlist: [noraInvited, maxWaiting],
};

const emptyLessons: MyLessons = { timezone: 'Europe/Berlin', now, upcoming: [], waitlist: [] };

/** A past lesson of the ballet class, `weeks` weeks before Monday's. */
const pastBallet = (weeks: number): ScheduleLesson => {
  const shift = (instant: string) =>
    new Date(Date.parse(instant) - weeks * 7 * 24 * 3600_000).toISOString();
  return {
    ...lessonBallet,
    id: id(150 + weeks),
    startsAt: shift(lessonBallet.startsAt),
    endsAt: shift(lessonBallet.endsAt),
  };
};

const past = (
  n: number,
  person: { id: string; name: string },
  lesson: ScheduleLesson,
  extra: Partial<Booking> = {},
): MyLessonBooking => ({
  booking: booking(n, person, lesson, extra),
  personName: person.name,
  lesson: summary(lesson),
  canCancel: false,
});

/** The first page of the history: the three months before now, newest first. */
const historyNow: MyLessonHistory = {
  timezone: 'Europe/Berlin',
  now,
  before: now,
  nextBefore: '2026-07-25T18:00:00Z',
  hasMore: true,
  items: [
    past(11, nora, pastBallet(1), { status: 'attended' }),
    past(12, lina, pastBallet(1), { status: 'no_show' }),
    past(13, nora, pastBallet(2), {
      status: 'canceled',
      late: true,
      canceledAt: '2026-10-11T12:00:00Z',
    }),
    past(14, max, pastBallet(3)),
    past(15, lina, { ...pastBallet(4), status: 'canceled' }),
  ],
};

/** The three months before: no bookings, but older ones exist. */
const historyEmpty: MyLessonHistory = {
  timezone: 'Europe/Berlin',
  now,
  before: '2026-07-25T18:00:00Z',
  nextBefore: '2026-04-25T18:00:00Z',
  hasMore: true,
  items: [],
};

/** The last page: one booking, nothing older. */
const historyLast: MyLessonHistory = {
  timezone: 'Europe/Berlin',
  now,
  before: '2026-04-25T18:00:00Z',
  nextBefore: '2026-01-25T18:00:00Z',
  hasMore: false,
  items: [past(21, nora, pastBallet(30), { status: 'attended' })],
};

const historyPages: Record<string, MyLessonHistory> = {
  '': historyNow,
  [historyNow.nextBefore]: historyEmpty,
  [historyEmpty.nextBefore]: historyLast,
};

/** The API as the member sees it; `lessons()` is read on every request. */
function mockMember({
  lessons = () => myLessons,
  history = (before: string) => historyPages[before]!,
  fam = family,
}: {
  lessons?: () => MyLessons;
  history?: (before: string) => MyLessonHistory;
  fam?: typeof family;
} = {}) {
  return mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: meMember },
    '/api/v1/family': { body: fam },
    '/api/v1/my/lessons': () => ({ body: lessons() }),
    '/api/v1/my/lessons/history': (init: MockRequest) => ({
      body: history((init.params?.query as { before?: string } | undefined)?.before ?? ''),
    }),
    '/api/v1/lessons/{lessonId}': (init: MockRequest) =>
      init.params?.path?.lessonId === ids.lessonBallet
        ? { body: lessonBalletDetail }
        : { status: 404, body: { title: 'Not Found' } },
    '/api/v1/bookings/{bookingId}/cancel-following': {
      body: {
        bookings: [
          {
            bookingId: id(601),
            lessonId: ids.lessonBallet,
            startsAt: lessonBallet.startsAt,
            endsAt: lessonBallet.endsAt,
            late: true,
          },
          {
            bookingId: id(611),
            lessonId: id(112),
            startsAt: '2026-11-02T17:00:00Z',
            endsAt: '2026-11-02T18:30:00Z',
            late: false,
          },
        ],
        until: '2027-07-31',
      },
    },
  });
}

function renderPage(search = {}) {
  render(<Toaster />);
  return renderAtMyLessons(<MyLessonsPage />, search);
}

/** The region of a section, by its heading. */
const region = async (name: string) => within(await screen.findByRole('region', { name }));

describe('MyLessonsPage', () => {
  it('lists the coming lessons of the family by day, with their names', async () => {
    mockMember();

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'My lessons' })).toBeVisible();
    const upcomingSection = await region('Coming up');
    // School dates (Berlin), whatever the test's zone; Monday is tomorrow.
    const monday = await upcomingSection.findByRole('group', {
      name: 'Tomorrow, Monday, October 26',
    });
    const mondayRows = within(monday).getAllByRole('listitem');
    expect(mondayRows).toHaveLength(2);
    expect(
      within(monday).getByRole('button', {
        name: 'Ballet, Monday, October 26, 18:00–19:30, Nora Example',
      }),
    ).toBeInTheDocument();
    expect(mondayRows[1]).toHaveTextContent('Lina Example');
    expect(mondayRows[0]).toHaveTextContent('Room A · North Studio');
    expect(within(mondayRows[0]!).getByText('Booked')).toBeInTheDocument();

    // The lesson the school canceled says so and has nothing to book or cancel.
    const tuesday = upcomingSection.getByRole('group', { name: 'Tuesday, October 27' });
    expect(within(tuesday).getByText('Canceled by the school')).toBeInTheDocument();
    expect(within(tuesday).queryByText('Booked')).not.toBeInTheDocument();

    const workshop = upcomingSection.getByRole('group', { name: 'Saturday, November 7' });
    expect(within(workshop).getByText('Max Example')).toBeInTheDocument();
    expect(within(workshop).getByText('Workshop')).toBeInTheDocument();
    // A one-off lesson has no following weeks.
    expect(
      within(workshop).queryByRole('button', { name: /^Also cancel the following weeks/ }),
    ).not.toBeInTheDocument();
    expect(
      within(workshop).getByRole('button', { name: 'Cancel the booking of Max Example' }),
    ).toBeInTheDocument();
  });

  it('leaves the names out for a family of one', async () => {
    mockMember({
      fam: alone,
      lessons: () => ({ ...myLessons, upcoming: [upcoming(1, nora, lessonBallet)] }),
    });

    renderPage();

    const upcomingSection = await region('Coming up');
    expect(
      await upcomingSection.findByRole('button', {
        name: 'Ballet, Monday, October 26, 18:00–19:30',
      }),
    ).toBeInTheDocument();
    expect(upcomingSection.queryByText('Nora Example')).not.toBeInTheDocument();
    expect(
      screen.getByText('Your bookings and waiting lists, and the lessons you went to.'),
    ).toBeInTheDocument();
  });

  it('cancels a booking after warning that it is late now', async () => {
    let current = myLessons;
    mockMember({ lessons: () => current });
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/cancel': () => {
        current = { ...myLessons, upcoming: myLessons.upcoming.slice(1) };
        return {
          body: booking(1, nora, lessonBallet, {
            status: 'canceled',
            late: true,
            canceledAt: now,
          }),
        };
      },
    });
    const user = userEvent.setup();

    renderPage();
    const upcomingSection = await region('Coming up');
    await user.click(
      await upcomingSection.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );
    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(
      within(confirm).getByText(
        'Nora Example will no longer be booked for Ballet, Monday, October 26, 18:00–19:30.',
      ),
    ).toBeInTheDocument();
    expect(within(confirm).getByText('This counts as a late cancellation.')).toBeInTheDocument();
    await user.click(within(confirm).getByRole('button', { name: 'Cancel booking' }));

    await waitFor(() => expect(confirm).not.toBeInTheDocument());
    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/cancel', {
      params: { path: { bookingId: id(601) } },
    });
    expect(
      await screen.findAllByText('The booking of Nora Example has been canceled.'),
    ).not.toHaveLength(0);
    // Asked again, the booking is gone from the list.
    await waitFor(() =>
      expect(
        upcomingSection.queryByRole('button', { name: 'Cancel the booking of Nora Example' }),
      ).not.toBeInTheDocument(),
    );
  });

  it('has no late warning before the cutoff', async () => {
    mockMember();
    const user = userEvent.setup();

    renderPage();
    const upcomingSection = await region('Coming up');
    await user.click(
      await upcomingSection.findByRole('button', { name: 'Cancel the booking of Max Example' }),
    );

    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(within(confirm).queryByText('This counts as a late cancellation.')).toBeNull();
  });

  it('offers no cancel for a lesson that has started', async () => {
    vi.setSystemTime(new Date('2026-10-26T17:10:00Z'));
    mockMember({
      lessons: () => ({
        ...myLessons,
        now: '2026-10-26T17:10:00Z',
        upcoming: [{ ...upcoming(1, nora, lessonBallet), canCancel: false }],
      }),
    });

    renderPage();

    const upcomingSection = await region('Coming up');
    const row = await upcomingSection.findByRole('listitem');
    expect(within(row).getByText('Now')).toBeInTheDocument();
    expect(within(row).queryByRole('button', { name: /^Cancel the booking/ })).toBeNull();
  });

  it('cancels the following weeks of a class', async () => {
    mockMember();
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/cancel-following': {
        body: { bookings: [{ bookingId: id(601), late: true }], until: '2027-07-31' },
      },
    });
    const user = userEvent.setup();

    renderPage();
    const upcomingSection = await region('Coming up');
    await user.click(
      await upcomingSection.findByRole('button', {
        name: 'Also cancel the following weeks of Nora Example',
      }),
    );

    const dialog = screen.getByRole('alertdialog', { name: 'Cancel the following weeks too?' });
    const items = within(await within(dialog).findByRole('list')).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('Monday, October 2618:00–19:30Late');
    await user.click(within(dialog).getByRole('button', { name: 'Cancel 2 bookings' }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/cancel-following', {
      params: { path: { bookingId: id(601) } },
      body: { until: '2027-07-31' },
    });
  });

  it('shows the waiting lists with the invite and the position', async () => {
    mockMember();

    renderPage();

    const waitlist = await region('Waiting lists');
    // Each entry is a card with its lesson and, in a list of its own, the entry.
    const rows = Array.from(waitlist.getAllByRole('list')[0]!.children) as HTMLElement[];
    expect(rows).toHaveLength(2);
    // The invite, until when in school time.
    expect(rows[0]).toHaveTextContent('Thu, Oct 29 · 18:30–20:00');
    expect(within(rows[0]!).getByText('Invited')).toBeInTheDocument();
    expect(
      within(rows[0]!).getByText('A place is reserved for you until Mon, Oct 26, 07:00.'),
    ).toBeInTheDocument();
    expect(within(rows[1]!).getByText('Waiting, position 2')).toBeInTheDocument();
    expect(
      within(rows[1]!).getByRole('button', { name: 'Leave the waiting list for Max Example' }),
    ).toBeInTheDocument();
  });

  it.each([
    ['accepts', 'Accept the place for Nora Example', '/api/v1/waitlist/{entryId}/accept'],
    ['declines', 'Decline the place for Nora Example', '/api/v1/waitlist/{entryId}/decline'],
  ])('%s an invite', async (_, button, path) => {
    let current = myLessons;
    mockMember({ lessons: () => current });
    const post = mockApiPost({
      [path]: () => {
        current = { ...myLessons, waitlist: [maxWaiting] };
        return path.endsWith('accept')
          ? { body: booking(9, nora, fullLesson) }
          : { body: { ...noraInvited.entry, status: 'expired', lessonId: fullLesson.id } };
      },
    });
    const user = userEvent.setup();

    renderPage();
    const waitlist = await region('Waiting lists');
    await user.click(await waitlist.findByRole('button', { name: button }));

    expect(post).toHaveBeenCalledWith(path, { params: { path: { entryId: id(701) } } });
    await waitFor(() => expect(waitlist.queryByText('Invited')).not.toBeInTheDocument());
  });

  it('leaves a waiting list', async () => {
    let current = myLessons;
    mockMember({ lessons: () => current });
    const del = mockApiDelete({
      '/api/v1/waitlist/{entryId}': () => {
        current = { ...myLessons, waitlist: [noraInvited] };
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    renderPage();
    const waitlist = await region('Waiting lists');
    await user.click(
      await waitlist.findByRole('button', { name: 'Leave the waiting list for Max Example' }),
    );

    expect(del).toHaveBeenCalledWith('/api/v1/waitlist/{entryId}', {
      params: { path: { entryId: id(702) } },
    });
    expect(await screen.findAllByText('Max Example has left the waiting list.')).not.toHaveLength(
      0,
    );
    await waitFor(() =>
      expect(waitlist.queryByText('Waiting, position 2')).not.toBeInTheDocument(),
    );
  });

  it('shows what became of past bookings and loads earlier months', async () => {
    const get = mockMember();
    const user = userEvent.setup();

    renderPage();
    const history = await region('History');
    const first = await history.findByRole('list');
    const rows = within(first).getAllByRole('listitem');
    expect(rows).toHaveLength(5);
    // Summer time in Berlin then.
    expect(rows[0]).toHaveTextContent('Mon, Oct 19 · 19:00–20:30');
    expect(within(rows[0]!).getByText('Attended')).toBeInTheDocument();
    expect(within(rows[1]!).getByText('No-show')).toBeInTheDocument();
    expect(within(rows[2]!).getByText('Canceled')).toBeInTheDocument();
    expect(within(rows[2]!).getByText('Late')).toBeInTheDocument();
    expect(within(rows[3]!).getByText('Not marked')).toBeInTheDocument();
    expect(within(rows[4]!).getByText('Canceled by the school')).toBeInTheDocument();
    expect(within(rows[4]!).queryByText('Not marked')).not.toBeInTheDocument();

    // Three months without bookings, but older ones exist.
    await user.click(history.getByRole('button', { name: 'Load earlier' }));
    expect(
      await history.findByText('No lessons between April 25, 2026 and July 25, 2026.'),
    ).toBeInTheDocument();
    expect(get).toHaveBeenCalledWith('/api/v1/my/lessons/history', {
      params: { query: { before: historyNow.nextBefore } },
      signal: expect.any(AbortSignal),
    });

    // The last page: nothing older, so nothing more to load.
    await user.click(history.getByRole('button', { name: 'Load earlier' }));
    expect(
      await history.findByText('Mon, Mar 30 · 19:00–20:30', { exact: false }),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(history.queryByRole('button', { name: 'Load earlier' })).not.toBeInTheDocument(),
    );
    expect(history.getAllByRole('list')).toHaveLength(2);
  });

  it('says when there are no lessons at all', async () => {
    mockMember({
      lessons: () => emptyLessons,
      history: () => ({ ...historyNow, items: [], hasMore: false }),
    });

    renderPage();

    expect(await (await region('Coming up')).findByText('No lessons coming up.')).toBeVisible();
    expect(
      (await region('Coming up')).getByRole('link', { name: 'Find a lesson in the schedule' }),
    ).toHaveAttribute('href', '/schedule');
    expect(await (await region('History')).findByText('No past lessons yet.')).toBeVisible();
    // Without entries, there are no waiting lists to show.
    expect(screen.queryByRole('region', { name: 'Waiting lists' })).not.toBeInTheDocument();
  });

  it('lists the waiting lists above the coming lessons', async () => {
    mockMember();

    renderPage();

    const waitlist = (await screen.findByRole('region', { name: 'Waiting lists' })) as Node;
    const upcomingSection = screen.getByRole('region', { name: 'Coming up' });
    expect(waitlist.compareDocumentPosition(upcomingSection)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );
  });

  it('drops a lesson that has ended and asks again, so it moves to the history', async () => {
    // Monday's ballet lesson (18:00–19:30 in Berlin) is about to end.
    vi.setSystemTime(new Date('2026-10-26T18:29:59.500Z'));
    const get = mockMember({
      fam: alone,
      // The server answers with its time; the lesson stays until the server sees it end.
      lessons: () => ({
        ...myLessons,
        now: new Date().toISOString(),
        upcoming: [
          { ...upcoming(1, nora, lessonBallet), canCancel: false },
          upcoming(4, max, lessonWorkshop),
        ],
        waitlist: [],
      }),
    });

    renderPage();
    const upcomingSection = await region('Coming up');
    const ballet = 'Ballet, Monday, October 26, 18:00–19:30';
    expect(await upcomingSection.findByRole('button', { name: ballet })).toBeInTheDocument();
    expect(upcomingSection.getByText('Now')).toBeInTheDocument();
    const asked = (p = '/api/v1/my/lessons') =>
      get.mock.calls.filter((call) => (call as unknown[])[0] === p).length;
    const before = asked();
    const historyBefore = asked('/api/v1/my/lessons/history');

    // It ends: My lessons is asked again then (the history gets it) …
    vi.setSystemTime(new Date('2026-10-26T18:30:30Z'));
    await waitFor(() => expect(asked()).toBeGreaterThan(before), { timeout: 3000 });
    await waitFor(() => expect(asked('/api/v1/my/lessons/history')).toBeGreaterThan(historyBefore));
    // … and by the clock, it is no longer coming up.
    act(() => void document.dispatchEvent(new Event('visibilitychange')));
    await waitFor(() =>
      expect(upcomingSection.queryByRole('button', { name: ballet })).not.toBeInTheDocument(),
    );
    expect(
      upcomingSection.getByRole('button', {
        name: 'Jazz, Saturday, November 7, 14:00–17:00, Workshop',
      }),
    ).toBeInTheDocument();
  });

  it('warns that canceling is late once the cutoff passes while the dialog is open', async () => {
    // Saturday 17:59 in Berlin: a minute before the cutoff of Monday's ballet lesson.
    vi.useRealTimers();
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] });
    vi.setSystemTime(new Date('2026-10-25T16:59:30Z'));
    mockMember({
      lessons: () => ({ ...myLessons, now: '2026-10-25T16:59:30Z' }),
    });

    renderPage();
    const upcomingSection = await region('Coming up');
    fireEvent.click(
      await upcomingSection.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );
    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(within(confirm).queryByText('This counts as a late cancellation.')).toBeNull();

    await act(() => vi.advanceTimersByTimeAsync(60_000));

    expect(within(confirm).getByText('This counts as a late cancellation.')).toBeInTheDocument();
  });

  it('shows the next four weeks and the later lessons on request, with their year', async () => {
    const later = (n: number, startsAt: string, endsAt: string): MyLessonBooking =>
      upcoming(n, nora, { ...lessonWorkshop, id: id(120 + n), startsAt, endsAt });
    mockMember({
      fam: alone,
      lessons: () => ({
        ...myLessons,
        upcoming: [
          upcoming(1, nora, lessonBallet),
          // Four weeks and more ahead: Monday, November 23, and in the next year.
          later(5, '2026-11-23T13:00:00Z', '2026-11-23T16:00:00Z'),
          later(6, '2027-01-09T13:00:00Z', '2027-01-09T16:00:00Z'),
        ],
        waitlist: [],
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const upcomingSection = await region('Coming up');
    expect(
      await upcomingSection.findByRole('group', { name: 'Tomorrow, Monday, October 26' }),
    ).toBeInTheDocument();
    expect(upcomingSection.getAllByRole('group')).toHaveLength(1);

    await user.click(upcomingSection.getByRole('button', { name: 'Show 2 later lessons' }));

    expect(upcomingSection.getByRole('group', { name: 'Monday, November 23' })).toBeInTheDocument();
    expect(
      upcomingSection.getByRole('group', { name: 'Saturday, January 9, 2027' }),
    ).toBeInTheDocument();
    expect(
      upcomingSection.queryByRole('button', { name: /^Show \d+ later lesson/ }),
    ).not.toBeInTheDocument();
  });

  it('says when booking has closed for a lesson the family waits for', async () => {
    // Thursday 17:45 in Berlin: booking for the full lesson (18:30) has closed.
    vi.setSystemTime(new Date('2026-10-29T16:45:00Z'));
    mockMember({
      lessons: () => ({
        ...myLessons,
        now: '2026-10-29T16:45:00Z',
        upcoming: [],
        waitlist: [maxWaiting],
      }),
    });

    renderPage();
    const waitlist = await region('Waiting lists');

    expect(await waitlist.findByText('Booking has closed')).toBeInTheDocument();
    expect(waitlist.getByText('No more invites will come for this lesson.')).toBeInTheDocument();
    expect(waitlist.queryByText('Waiting, position 2')).not.toBeInTheDocument();
    // The entry may still be left until the lesson starts.
    expect(
      waitlist.getByRole('button', { name: 'Leave the waiting list for Max Example' }),
    ).toBeInTheDocument();
  });

  it('opens a lesson in the lesson sheet', async () => {
    mockMember();
    const user = userEvent.setup();

    const { router } = renderPage();
    const upcomingSection = await region('Coming up');
    await user.click(
      await upcomingSection.findByRole('button', {
        name: 'Ballet, Monday, October 26, 18:00–19:30, Nora Example',
      }),
    );

    await waitFor(() => expect(router.state.location.search).toEqual({ lesson: ids.lessonBallet }));
    expect(await screen.findByRole('dialog', { name: 'Ballet' })).toBeInTheDocument();
  });

  it('asks visitors to sign in', async () => {
    mockApiGet(signedOut);

    renderPage();

    expect(await screen.findByText('Sign in to see your lessons')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(screen.queryByRole('region', { name: 'Coming up' })).not.toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockMember();
    await i18n.changeLanguage('de');

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Meine Stunden' })).toBeVisible();
    const upcomingSection = await region('Demnächst');
    expect(
      await upcomingSection.findByRole('group', { name: 'Morgen, Montag, 26. Oktober' }),
    ).toBeInTheDocument();
    expect(upcomingSection.getByText('Von der Schule abgesagt')).toBeInTheDocument();
    expect(
      upcomingSection.getByRole('button', { name: 'Buchung von Nora Example stornieren' }),
    ).toBeInTheDocument();
    expect(await (await region('Wartelisten')).findByText('Eingeladen')).toBeInTheDocument();
    const history = await region('Verlauf');
    expect(await history.findByText('Anwesend')).toBeInTheDocument();
    expect(history.getByRole('button', { name: 'Frühere laden' })).toBeInTheDocument();
  });
});
