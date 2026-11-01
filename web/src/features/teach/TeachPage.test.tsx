import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '@/api/client';
import type { TeachLesson, TeachLessons } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  ids,
  lessonBalletDetail,
  meMember,
  meAdmin,
  meOffice,
  type MockRequest,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  signedOut,
  staffEntries,
  tooManyRequests,
} from '@/test/mockApi';
import { renderAtTeach } from '@/test/utils';
import { TeachPage } from './TeachPage';

// The server's clock: Wednesday 2026-10-28, 11:00 in Berlin. The device's
// clock is hours behind, so whatever depends on "now" must follow the server.
const serverNow = '2026-10-28T10:00:00Z';
const deviceNow = '2026-10-28T04:00:00Z';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(deviceNow));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

/** The fictional instructor Alex, as GET /auth/me returns him. */
const meInstructor: Me = {
  accountId: id(361),
  personId: ids.personAlex,
  email: 'alex.example@example.com',
  role: 'instructor',
  givenName: 'Alex',
  familyName: 'Example',
  teaches: true,
};

const lesson = (n: number, extra: Partial<TeachLesson>): TeachLesson => ({
  id: id(900 + n),
  courseType: { id: ids.ballet, name: 'Ballet' },
  room: { id: ids.roomA, name: 'Room A', position: 1 },
  location: { id: ids.locationNorth, name: 'North Studio' },
  instructor: { id: ids.staffAlex, displayName: 'Alex Example' },
  startsAt: '2026-10-28T09:30:00Z',
  endsAt: '2026-10-28T11:00:00Z',
  status: 'scheduled',
  kind: 'regular',
  capacity: 12,
  visibility: 'public',
  role: 'instructor',
  active: 0,
  held: 0,
  freePlaces: 12,
  waiting: 0,
  trials: 0,
  guests: 0,
  dayOverAt: '2026-10-28T23:00:00Z',
  attendanceOpen: false,
  attendanceTaken: false,
  ...extra,
});

/** Monday's class: its day is over and nobody took attendance. */
const monday = lesson(1, {
  startsAt: '2026-10-26T17:00:00Z',
  endsAt: '2026-10-26T18:30:00Z',
  dayOverAt: '2026-10-26T23:00:00Z',
  active: 3,
  freePlaces: 9,
});
/** Tuesday's contemporary class, which Alex accompanied; attendance taken. */
const tuesday = lesson(2, {
  courseType: { id: ids.contemporary, name: 'Contemporary' },
  instructor: { id: ids.staffRiley, displayName: 'Riley Sample' },
  accompanist: { id: ids.staffAlex, displayName: 'Alex Example' },
  role: 'accompanist',
  startsAt: '2026-10-27T16:00:00Z',
  endsAt: '2026-10-27T17:00:00Z',
  dayOverAt: '2026-10-27T23:00:00Z',
  active: 4,
  attendanceTaken: true,
});
/**
 * This morning's ballet class (10:30–12:00 in Berlin), running now. The
 * list is from before its start, so the server did not open attendance yet.
 */
const morning = lesson(3, {
  id: ids.lessonBallet,
  active: 5,
  freePlaces: 5,
  waiting: 2,
  trials: 1,
  guests: 2,
  held: 2,
});
/** Tonight's jazz class, canceled. */
const evening = lesson(4, {
  courseType: { id: ids.jazz, name: 'Jazz' },
  startsAt: '2026-10-28T17:00:00Z',
  endsAt: '2026-10-28T18:00:00Z',
  status: 'canceled',
});
/** Tomorrow's hidden workshop. */
const tomorrow = lesson(5, {
  kind: 'workshop',
  visibility: 'hidden',
  startsAt: '2026-10-29T13:00:00Z',
  endsAt: '2026-10-29T15:00:00Z',
  dayOverAt: '2026-10-29T23:00:00Z',
});

const teach = (extra: Partial<TeachLessons> = {}): TeachLessons => ({
  timezone: 'Europe/Berlin',
  now: serverNow,
  from: '2026-10-21',
  to: '2026-11-05',
  lessons: [monday, tuesday, morning, evening, tomorrow],
  ...extra,
});

type Query = { from?: string; to?: string; staffId?: string };

/** The instructor view: GET /teach answers with the range it was asked for. */
function mockTeach(me: Me | null, lessons: (q: Query) => TeachLessons = () => teach()) {
  return mockApiGet({
    ...(me ? { '/api/v1/auth/me': { body: me } } : signedOut),
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/teach': (init: MockRequest) => ({ body: lessons(init.params?.query ?? {}) }),
    '/api/v1/admin/staff': { body: { items: staffEntries } },
    '/api/v1/lessons/{lessonId}': (init: MockRequest) =>
      init.params?.path?.lessonId === ids.lessonBallet
        ? { body: lessonBalletDetail }
        : problem(404, 'not found'),
  });
}

/** The GET /teach requests made so far, by their query. */
const teachRequests = (get: ReturnType<typeof mockTeach>) =>
  (get.mock.calls as unknown as [string, MockRequest][])
    .filter(([path]) => path === '/api/v1/teach')
    .map(([, init]) => init.params?.query);

/** The row of a lesson: its list item, found by the lesson's button. */
const row = (name: RegExp) => screen.getByRole('button', { name }).closest('li') as HTMLElement;

describe('TeachPage', () => {
  it('groups the lessons by day, folds the earlier days away and marks today', async () => {
    mockTeach(meInstructor);
    const user = userEvent.setup();

    renderAtTeach(<TeachPage />);

    expect(await screen.findByRole('heading', { level: 1, name: 'Teaching' })).toBeInTheDocument();
    const today = await screen.findByRole('heading', {
      level: 2,
      name: 'Today, Wednesday, October 28',
    });
    expect(today).toHaveAttribute('aria-current', 'date');
    expect(
      screen.getByRole('heading', { level: 2, name: 'Tomorrow, Thursday, October 29' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Oct 21 – Nov 4')).toBeInTheDocument();

    // Monday and Tuesday are folded away, with what is missing there.
    const earlier = screen.getByRole('button', {
      name: 'Earlier: 2 lessons · 1 without attendance',
    });
    expect(earlier).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('heading', { name: /Monday/ })).not.toBeInTheDocument();

    await user.click(earlier);
    expect(earlier).toHaveAttribute('aria-expanded', 'true');
    expect(
      screen.getByRole('heading', { level: 2, name: 'Monday, October 26' }),
    ).toBeInTheDocument();
    expect(within(row(/^Ballet, Monday/)).getByText('Attendance missing')).toBeInTheDocument();
    const tue = within(row(/^Contemporary, Tuesday/));
    expect(tue.getByText('Attendance taken')).toBeInTheDocument();
    expect(tue.getByText('Accompanist')).toBeInTheDocument();
    expect(row(/^Contemporary, Tuesday/)).toHaveTextContent(
      'Riley Sample · accompanied by Alex Example',
    );
  });

  it('shows roles, counts and markers, and the attendance by the server clock', async () => {
    mockTeach(meInstructor);

    renderAtTeach(<TeachPage />);

    await screen.findByRole('heading', { level: 2, name: /^Today/ });
    const m = within(row(/^Ballet, Wednesday, October 28, 10:30–12:00$/));
    expect(m.getByText('Instructor')).toBeInTheDocument();
    expect(m.getByText('5/12 booked · 2 waiting · 1 trial · 2 guests')).toBeInTheDocument();
    // Started by the server's clock, though not by the device's.
    expect(m.getByText('Attendance open')).toBeInTheDocument();
    expect(
      m.getByRole('button', {
        name: 'Message participants of Ballet, Wednesday, October 28, 10:30–12:00',
      }),
    ).toBeInTheDocument();

    const e = within(row(/^Jazz, Wednesday, October 28, 18:00–19:00, Canceled$/));
    expect(e.getByText('Canceled')).toBeInTheDocument();
    expect(e.getByText('0/12 booked')).toBeInTheDocument();
    expect(e.queryByText(/^Attendance/)).not.toBeInTheDocument();
    // Nobody to write to.
    expect(e.queryByRole('button', { name: /^Message participants/ })).not.toBeInTheDocument();

    const t = within(row(/^Ballet, Thursday, October 29, 14:00–16:00, Workshop, Hidden/));
    expect(t.getByText('Hidden – staff only')).toBeInTheDocument();
    expect(t.queryByText(/^Attendance/)).not.toBeInTheDocument();

    // Tuesday's class, which Alex accompanied, is folded away under "Earlier".
    expect(screen.queryAllByText('Accompanist')).toHaveLength(0);
  });

  it.each([
    ['an instructor', meInstructor, 'Attendance missing', 1],
    // The office and admins take attendance at any time, on their own lessons too.
    ['an admin who teaches the lesson', meAdmin, 'Attendance open', 0],
  ] as const)(
    'shows the attendance of a lesson whose day is over to %s',
    async (_, me, state, missing) => {
      vi.setSystemTime(new Date('2026-10-28T22:30:00Z'));
      mockTeach(me, () =>
        teach({ now: '2026-10-28T23:30:00Z', lessons: [{ ...morning, role: 'instructor' }] }),
      );
      const user = userEvent.setup();

      renderAtTeach(<TeachPage />);

      // Past midnight in Berlin: Wednesday is an earlier day now.
      await user.click(
        await screen.findByRole('button', {
          name: missing ? 'Earlier: 1 lesson · 1 without attendance' : 'Earlier: 1 lesson',
        }),
      );
      expect(within(row(/^Ballet, .*10:30–12:00$/)).getByText(state)).toBeInTheDocument();
    },
  );

  it('keeps the server clock while the next range loads', async () => {
    const get = mockTeach(meInstructor);
    type Get = (path: string, init?: MockRequest) => Promise<unknown>;
    const loose = get as unknown as {
      getMockImplementation(): Get;
      mockImplementation(f: Get): void;
    };
    const answer = loose.getMockImplementation();
    // The next range never arrives: the current one stays, as a placeholder.
    loose.mockImplementation((path, init) =>
      path === '/api/v1/teach' && init?.params?.query?.from
        ? new Promise(() => {})
        : answer(path, init),
    );
    const user = userEvent.setup();

    renderAtTeach(<TeachPage />);
    await screen.findByRole('heading', { level: 2, name: 'Today, Wednesday, October 28' });

    await user.click(screen.getByRole('button', { name: 'Next 2 weeks' }));
    expect(await screen.findByText('Nov 4 – Nov 18')).toBeInTheDocument();
    expect(teachRequests(get).at(-1)).toEqual({ from: '2026-11-04', to: '2026-11-19' });
    // The previous list, dimmed, with today and the attendance as before.
    expect(
      screen.getByRole('heading', { level: 2, name: 'Today, Wednesday, October 28' }),
    ).toBeInTheDocument();
    expect(
      within(row(/^Ballet, Wednesday, October 28, 10:30–12:00$/)).getByText('Attendance open'),
    ).toBeInTheDocument();
  });

  it('moves the range by two weeks and back to today', async () => {
    const get = mockTeach(meInstructor, (q) =>
      teach(q.from ? { from: q.from, to: q.to ?? '', lessons: [] } : {}),
    );
    const user = userEvent.setup();

    const { router } = renderAtTeach(<TeachPage />);
    await screen.findByText('Oct 21 – Nov 4');
    expect(teachRequests(get)).toEqual([{}]);

    await user.click(screen.getByRole('button', { name: 'Next 2 weeks' }));
    expect(await screen.findByText('Nov 4 – Nov 18')).toBeInTheDocument();
    expect(teachRequests(get).at(-1)).toEqual({ from: '2026-11-04', to: '2026-11-19' });
    expect(router.state.location.search).toEqual({ from: '2026-11-04' });
    expect(await screen.findByText('No lessons on these days.')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Previous 2 weeks' }));
    await user.click(screen.getByRole('button', { name: 'Previous 2 weeks' }));
    expect(await screen.findByText('Oct 7 – Oct 21')).toBeInTheDocument();
    expect(teachRequests(get).at(-1)).toEqual({ from: '2026-10-07', to: '2026-10-22' });

    await user.click(screen.getByRole('button', { name: 'Today' }));
    expect(await screen.findByText('Oct 21 – Nov 4')).toBeInTheDocument();
    expect(router.state.location.search).toEqual({});
    expect(screen.getByRole('button', { name: 'Today' })).toBeDisabled();
  });

  it('lets the office narrow the lessons to one staff member', async () => {
    const get = mockTeach(meOffice, (q) =>
      teach({
        lessons: [monday, morning, tuesday, evening, tomorrow]
          .map((l) => ({ ...l, role: 'office' as const }))
          .filter(
            (l) => !q.staffId || l.instructor?.id === q.staffId || l.accompanist?.id === q.staffId,
          ),
      }),
    );
    const user = userEvent.setup();

    const { router } = renderAtTeach(<TeachPage />);
    expect(await screen.findByText(/^Every lesson of the school/)).toBeInTheDocument();
    await screen.findByRole('button', { name: /^Jazz, Wednesday/ });
    expect(screen.queryByText('Instructor')).not.toBeInTheDocument();

    await user.click(screen.getByRole('combobox', { name: 'Staff' }));
    expect(
      await screen.findByRole('option', { name: 'Quinn Former (former)' }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole('option', { name: 'Riley Sample' }));

    await waitFor(() => expect(teachRequests(get).at(-1)).toEqual({ staffId: ids.staffRiley }));
    expect(router.state.location.search).toEqual({ staff: ids.staffRiley });
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /^Jazz, Wednesday/ })).not.toBeInTheDocument(),
    );

    await user.click(screen.getByRole('combobox', { name: 'Staff' }));
    await user.click(await screen.findByRole('option', { name: 'All staff' }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(await screen.findByRole('button', { name: /^Jazz, Wednesday/ })).toBeInTheDocument();
  });

  it('marks today even without lessons on it', async () => {
    mockTeach(meInstructor, () => teach({ lessons: [monday, tomorrow] }));

    renderAtTeach(<TeachPage />);

    const today = await screen.findByRole('heading', {
      level: 2,
      name: 'Today, Wednesday, October 28',
    });
    expect(today).toHaveAttribute('aria-current', 'date');
    expect(screen.getByText('No lessons today.')).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { level: 2, name: 'Tomorrow, Thursday, October 29' }),
    ).toBeInTheDocument();
  });

  it('offers the teaching feed only to those who teach', async () => {
    mockTeach(meInstructor);
    const first = renderAtTeach(<TeachPage />);
    await screen.findByRole('heading', { level: 2, name: /^Today/ });
    expect(screen.getByRole('button', { name: 'Subscribe' })).toBeInTheDocument();
    first.unmount();

    vi.restoreAllMocks();
    mockTeach(meOffice);
    renderAtTeach(<TeachPage />);
    await screen.findByRole('heading', { level: 2, name: /^Today/ });
    expect(screen.queryByRole('button', { name: 'Subscribe' })).not.toBeInTheDocument();
  });

  it('has no staff filter for instructors', async () => {
    const get = mockTeach(meInstructor);

    renderAtTeach(<TeachPage />, { staff: ids.staffRiley });

    await screen.findByRole('heading', { level: 2, name: /^Today/ });
    expect(screen.queryByRole('combobox', { name: 'Staff' })).not.toBeInTheDocument();
    expect(teachRequests(get)).toEqual([{}]);
  });

  it('opens a lesson in the lesson sheet', async () => {
    mockTeach(meInstructor);
    const user = userEvent.setup();

    const { router } = renderAtTeach(<TeachPage />);
    await user.click(
      await screen.findByRole('button', { name: 'Ballet, Wednesday, October 28, 10:30–12:00' }),
    );

    expect(router.state.location.search).toEqual({ lesson: ids.lessonBallet });
    const sheet = await screen.findByRole('dialog', { name: 'Ballet' });
    expect(within(sheet).getByText('Room A')).toBeInTheDocument();

    await user.click(within(sheet).getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });

  it.each([
    ['a member', meMember],
    ['a visitor', null],
  ])('is not for %s', async (_, me) => {
    const get = mockTeach(me);

    renderAtTeach(<TeachPage />);

    expect(await screen.findByRole('heading', { level: 1, name: 'No access' })).toBeInTheDocument();
    expect(
      screen.getByText('Only instructors, the office and administrators can see this page.'),
    ).toBeInTheDocument();
    expect(teachRequests(get)).toEqual([]);
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    mockTeach(meInstructor);

    renderAtTeach(<TeachPage />);

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Unterricht' }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole('heading', { level: 2, name: 'Heute, Mittwoch, 28. Oktober' }),
    ).toBeInTheDocument();
    expect(
      screen.getByText('5/12 gebucht · 2 warten · 1 Probestunde · 2 Gäste'),
    ).toBeInTheDocument();
    expect(screen.getByText('Anwesenheit offen')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Nächste 2 Wochen' })).toBeInTheDocument();
  });
});

describe('NotifyDialog', () => {
  const label = 'Ballet, Wednesday, October 28, 10:30–12:00';

  /** Opens the message dialog of the morning class; returns it. */
  async function openDialog(user: ReturnType<typeof userEvent.setup>) {
    render(<Toaster />);
    renderAtTeach(<TeachPage />);
    await user.click(
      await screen.findByRole('button', { name: `Message participants of ${label}` }),
    );
    return screen.findByRole('dialog', { name: 'Message participants' });
  }

  it('sends a message and says to how many it went', async () => {
    mockTeach(meInstructor);
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/notify': { body: { recipients: 3 } },
    });
    const user = userEvent.setup();

    const dialog = await openDialog(user);
    expect(
      within(dialog).getByText(new RegExp(`^${label}\\. The message goes`)),
    ).toBeInTheDocument();
    await user.type(within(dialog).getByRole('textbox', { name: 'Message' }), '  Bring water.  ');
    await user.click(within(dialog).getByRole('button', { name: 'Send' }));

    expect(await screen.findByText('Sent to 3 recipients.')).toBeInTheDocument();
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/notify', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { message: 'Bring water.' },
    });
  });

  it('says when nobody could be reached', async () => {
    mockTeach(meInstructor);
    mockApiPost({ '/api/v1/lessons/{lessonId}/notify': { body: { recipients: 0 } } });
    const user = userEvent.setup();

    const dialog = await openDialog(user);
    await user.type(within(dialog).getByRole('textbox', { name: 'Message' }), 'Hello');
    await user.click(within(dialog).getByRole('button', { name: 'Send' }));

    expect(
      await screen.findByText('Nobody could be reached: no participant has an email address.'),
    ).toBeInTheDocument();
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
  });

  it('counts characters in code points and refuses an empty or too long message', async () => {
    mockTeach(meInstructor);
    const post = mockApiPost({});
    const user = userEvent.setup();

    const dialog = await openDialog(user);
    const field = within(dialog).getByRole('textbox', { name: 'Message' });
    expect(within(dialog).getByText('0/1000')).toBeInTheDocument();

    await user.click(within(dialog).getByRole('button', { name: 'Send' }));
    expect(await within(dialog).findByText('Write a message.')).toBeInTheDocument();
    expect(field).toHaveAttribute('aria-invalid', 'true');

    // Two emoji and a line break: three characters, though seven UTF-16 units.
    fireEvent.change(field, { target: { value: '🩰\r\n🎹' } });
    expect(within(dialog).getByText('3/1000')).toBeInTheDocument();
    expect(field).toHaveAccessibleDescription(/3 of 1000 characters/);

    fireEvent.change(field, { target: { value: '🩰'.repeat(1001) } });
    expect(within(dialog).getByText('1001/1000')).toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: 'Send' }));
    expect(
      await within(dialog).findByText('The message is too long: at most 1000 characters.'),
    ).toBeInTheDocument();

    fireEvent.change(field, { target: { value: '🩰'.repeat(1000) } });
    expect(within(dialog).getByText('1000/1000')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('marks the message the API refuses', async () => {
    mockTeach(meInstructor);
    mockApiPost({
      '/api/v1/lessons/{lessonId}/notify': problem(422, 'validation failed', [
        { location: 'body.message', message: 'contains control characters' },
      ]),
    });
    const user = userEvent.setup();

    const dialog = await openDialog(user);
    const field = within(dialog).getByRole('textbox', { name: 'Message' });
    await user.type(field, 'Hello');
    await user.click(within(dialog).getByRole('button', { name: 'Send' }));

    expect(
      await within(dialog).findByText(
        'The message cannot be sent like this: use plain text of at most 1000 characters.',
      ),
    ).toBeInTheDocument();
    expect(field).toHaveAttribute('aria-invalid', 'true');
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument();
  });

  it.each([
    ['the header', tooManyRequests(1500), 'Please try again in 25 minutes.'],
    ['the detail', tooManyRequests(45, { header: false }), 'Please try again in 45 seconds.'],
    ['the header, in hours', tooManyRequests(5 * 3600 + 60), 'Please try again in 6 hours.'],
  ])('says how long to wait after too many messages (from %s)', async (_, answer, wait) => {
    mockTeach(meInstructor);
    mockApiPost({ '/api/v1/lessons/{lessonId}/notify': answer });
    const user = userEvent.setup();

    const dialog = await openDialog(user);
    await user.type(within(dialog).getByRole('textbox', { name: 'Message' }), 'Hello');
    await user.click(within(dialog).getByRole('button', { name: 'Send' }));

    const alert = await within(dialog).findByRole('alert');
    expect(alert).toHaveTextContent('Too many messages.');
    expect(alert).toHaveTextContent(wait);
    expect(dialog).toBeInTheDocument();
  });
});
