import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, type Schedule, type ScheduleLesson } from '@/api/client';
import i18n from '@/i18n';
import {
  ids,
  lessonBallet,
  lessonContemporary,
  lessonWorkshop,
  family,
  meMember,
  mockApiGet,
  schedule,
  signedOut,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import type { DayHeadingProps, LessonCardProps, Theme } from '@/themes/slots';
import { ThemeContext } from '@/themes/themeContext';
import type { ScheduleSearch } from '../search';
import { ScheduleList } from './ScheduleList';

/** Ballet at 00:30–01:30 Berlin on Tuesday 2026-10-27 (still Monday evening in New York and UTC). */
const lessonLate: ScheduleLesson = {
  ...lessonBallet,
  id: '0190a000-0000-7000-8000-000000000104',
  startsAt: '2026-10-26T23:30:00Z',
  endsAt: '2026-10-27T00:30:00Z',
};

const withLate: Schedule = {
  ...schedule,
  lessons: [lessonBallet, lessonLate, lessonContemporary, lessonWorkshop],
};

const empty: Schedule = { ...schedule, lessons: [] };

afterEach(() => {
  vi.restoreAllMocks();
});

/**
 * Renders the list as if the server answered at `serverNow` (by default the
 * fixture's Friday 2026-10-23, before every lesson) just now.
 */
function renderList(list: Schedule, search?: ScheduleSearch, serverNow = list.now) {
  return renderAtSchedule(
    <ScheduleList schedule={{ ...list, now: serverNow }} receivedAt={Date.now()} />,
    search,
  );
}

function day(name: string) {
  return within(screen.getByRole('region', { name }));
}

const lina = { personId: '0190a000-0000-7000-8000-000000000331', name: 'Lina Example' };
const noraBooked = {
  personId: meMember.personId,
  name: 'Nora Example',
  bookingId: '0190a000-0000-7000-8000-000000000601',
};
const linaBooked = { ...lina, bookingId: '0190a000-0000-7000-8000-000000000602' };

describe('ScheduleList', () => {
  it('shows the free places and whom of the family a lesson is booked for', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meMember } });
    renderList({
      ...schedule,
      lessons: [
        { ...lessonBallet, freePlaces: 3, bookedFor: [noraBooked, linaBooked] },
        { ...lessonContemporary, bookedFor: [noraBooked] },
        { ...lessonWorkshop, freePlaces: 0, bookedFor: [linaBooked] },
      ],
    });

    const ballet = await screen.findByRole('button', { name: /^Ballet/ });
    expect(await within(ballet).findByText('Booked: you, Lina Example')).toBeInTheDocument();
    expect(within(ballet).getByText('3 free')).toBeInTheDocument();
    expect(ballet).toHaveAccessibleDescription(/3 free$/);
    // A canceled lesson keeps its bookings but has no places to offer.
    const contemporary = screen.getByRole('button', { name: /^Contemporary/ });
    expect(within(contemporary).getByText('Booked')).toBeInTheDocument();
    expect(within(contemporary).queryByText(/free$/)).not.toBeInTheDocument();
    const workshop = screen.getByRole('button', { name: /^Jazz/ });
    expect(within(workshop).getByText('Booked: Lina Example')).toBeInTheDocument();
    expect(within(workshop).getByText('Full')).toBeInTheDocument();
  });

  it('shows whom of the family a lesson has on its waiting list or invited', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meMember } });
    renderList({
      ...schedule,
      lessons: [
        {
          ...lessonBallet,
          freePlaces: 0,
          waitlist: [
            {
              ...noraBooked,
              entryId: noraBooked.bookingId,
              status: 'waiting',
              position: 2,
              stayOnList: false,
            },
          ],
        },
        {
          ...lessonWorkshop,
          freePlaces: 0,
          waitlist: [
            {
              ...lina,
              entryId: linaBooked.bookingId,
              status: 'invited',
              expiresAt: '2026-10-24T10:00:00Z',
              stayOnList: false,
            },
          ],
        },
      ],
    });

    const ballet = await screen.findByRole('button', { name: /^Ballet/ });
    expect(within(ballet).getByText('Waiting list')).toBeInTheDocument();
    expect(ballet).toHaveAccessibleDescription(/Waiting list Full$/);
    const workshop = screen.getByRole('button', { name: /^Jazz/ });
    expect(await within(workshop).findByText('Invited: Lina Example')).toBeInTheDocument();
    expect(workshop).toHaveAccessibleDescription(/Invited: Lina Example Full$/);
  });

  it("drops the invited badge once the invite has run out by the server's clock", async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meMember } });
    renderList(
      {
        ...schedule,
        lessons: [
          {
            ...lessonWorkshop,
            freePlaces: 0,
            waitlist: [
              {
                ...lina,
                entryId: linaBooked.bookingId,
                status: 'invited',
                expiresAt: '2026-10-24T10:00:00Z',
                stayOnList: false,
              },
            ],
          },
        ],
      },
      undefined,
      // Saturday noon in Berlin: two hours after the invite ran out.
      '2026-10-24T12:00:00Z',
    );

    const workshop = await screen.findByRole('button', { name: /^Jazz/ });
    expect(within(workshop).getByText('Full')).toBeInTheDocument();
    expect(within(workshop).queryByText(/^Invited/)).not.toBeInTheDocument();
    expect(workshop).toHaveAccessibleDescription(/Full$/);
  });

  it('shows a plain "Booked" until it knows who is signed in', async () => {
    // GET /auth/me never answers.
    vi.spyOn(api, 'GET').mockImplementation((() => new Promise(() => undefined)) as never);
    renderList({ ...schedule, lessons: [{ ...lessonBallet, bookedFor: [noraBooked] }] });

    const ballet = await screen.findByRole('button', { name: /^Ballet/ });
    expect(within(ballet).getByText('Booked')).toBeInTheDocument();
    expect(within(ballet).queryByText(/Nora/)).not.toBeInTheDocument();
  });

  it('refers only to badges it shows', async () => {
    // Tuesday noon by the server: the ballet is over, nobody booked it.
    renderList(schedule, undefined, '2026-10-27T11:00:00Z');

    const ballet = await screen.findByRole('button', { name: /^Ballet/ });
    expect(within(ballet).queryByText(/free$/)).not.toBeInTheDocument();
    const ids = (ballet.getAttribute('aria-describedby') ?? '').split(' ');
    expect(ids).toHaveLength(1);
    expect(document.getElementById(ids[0]!)).not.toBeNull();
  });

  it('shows no booked badge for a lesson nobody of the family booked', async () => {
    renderList(schedule);

    const ballet = await screen.findByRole('button', { name: /^Ballet/ });
    expect(within(ballet).getByText('12 free')).toBeInTheDocument();
    expect(within(ballet).queryByText(/^Booked/)).not.toBeInTheDocument();
  });

  it('groups lessons by school day with headings and times in the school time zone', async () => {
    renderList(withLate);

    const headings = await screen.findAllByRole('heading', { level: 2 });
    expect(headings.map((h) => h.textContent)).toEqual([
      'Monday, October 26',
      'Tuesday, October 27',
      'Saturday, November 7',
    ]);

    const monday = day('Monday, October 26');
    expect(monday.getAllByRole('button')).toHaveLength(1);
    expect(monday.getByText('18:00–19:30')).toBeInTheDocument();

    const tuesday = day('Tuesday, October 27');
    expect(tuesday.getAllByRole('button')).toHaveLength(2);
    expect(tuesday.getByText('00:30–01:30')).toBeInTheDocument();
    expect(tuesday.getByText('17:00–18:00')).toBeInTheDocument();
    expect(
      tuesday.getByRole('button', { name: 'Ballet, Tuesday, October 27, 00:30–01:30' }),
    ).toBeInTheDocument();
  });

  it('shows course type, level, place and staff of a lesson', async () => {
    renderList(schedule);

    const card = within(
      await screen.findByRole('button', { name: 'Ballet, Monday, October 26, 18:00–19:30' }),
    );
    expect(card.getByText('Ballet')).toBeInTheDocument();
    expect(card.getByText('Beginner')).toBeInTheDocument();
    expect(card.getByText(/Room A · North Studio/)).toBeInTheDocument();
    expect(card.getByText('Alex Example')).toBeInTheDocument();
    expect(card.getByText(/accompanied by Sam Pianist/)).toBeInTheDocument();
  });

  it('lists canceled lessons with their status and workshops with their kind', async () => {
    renderList(schedule);

    const canceled = await screen.findByRole('button', {
      name: 'Contemporary, Tuesday, October 27, 17:00–18:00, Canceled',
    });
    expect(within(canceled).getByText('Canceled')).toBeInTheDocument();
    expect(within(canceled).getByText('Intermediate')).toBeInTheDocument();

    const workshop = screen.getByRole('button', {
      name: 'Jazz, Saturday, November 7, 14:00–17:00, Workshop',
    });
    expect(within(workshop).getByText('Workshop')).toBeInTheDocument();
    for (const level of ['Beginner', 'Intermediate', 'Advanced', 'Professional', 'All levels']) {
      expect(within(workshop).queryByText(level)).not.toBeInTheDocument();
    }
  });

  it('marks lessons for members and hidden lessons', async () => {
    const restricted: Schedule = {
      ...schedule,
      lessons: [
        { ...lessonBallet, visibility: 'members' },
        { ...lessonWorkshop, visibility: 'hidden' },
      ],
    };
    renderList(restricted);

    const members = await screen.findByRole('button', {
      name: 'Ballet, Monday, October 26, 18:00–19:30, Members only',
    });
    expect(within(members).getByText('Members only')).toBeInTheDocument();
    const hidden = screen.getByRole('button', {
      name: 'Jazz, Saturday, November 7, 14:00–17:00, Workshop, Hidden – staff only',
    });
    expect(within(hidden).getByText('Hidden – staff only')).toBeInTheDocument();
  });

  it('shows no visibility badge on public lessons', async () => {
    renderList(schedule);

    expect(await screen.findAllByRole('button', { name: /\d{2}:\d{2}–/ })).toHaveLength(3);
    expect(screen.queryByText('Members only')).not.toBeInTheDocument();
    expect(screen.queryByText(/^Hidden/)).not.toBeInTheDocument();
  });

  it('opens the lesson detail by writing the lesson id into the URL', async () => {
    const user = userEvent.setup();
    const { router } = renderList(schedule, {
      level: 'beginner',
    });

    await user.click(
      await screen.findByRole('button', { name: 'Ballet, Monday, October 26, 18:00–19:30' }),
    );

    expect(router.state.location.search).toEqual({
      level: 'beginner',
      lesson: ids.lessonBallet,
    });
  });

  it('explains an empty range and shows the next release', async () => {
    renderList(empty);

    expect(await screen.findByText('No lessons in this period')).toBeInTheDocument();
    expect(
      screen.getByText('No lessons have been published for these dates yet.'),
    ).toBeInTheDocument();
    // 2026-10-25T17:00Z is 18:00 in Berlin (CET), 13:00 in New York.
    expect(screen.getByText('Next release: Sun, Oct 25, 18:00')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reset filters' })).not.toBeInTheDocument();
  });

  it('offers to reset active filters when nothing matches', async () => {
    const user = userEvent.setup();
    const { router } = renderList(empty, {
      view: 'list',
      level: 'advanced',
      location: ids.locationSouth,
    });

    expect(await screen.findByText('No lessons match the selected filters.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Reset filters' }));

    expect(router.state.location.search).toEqual({ view: 'list' });
  });

  it('colors the day headings and names today and tomorrow', async () => {
    renderList(withLate, {}, '2026-10-26T10:00:00Z');

    const headings = await screen.findAllByRole('heading', { level: 2 });
    expect(headings.map((h) => h.textContent)).toEqual([
      'Today, Monday, October 26',
      'Tomorrow, Tuesday, October 27',
      'Saturday, November 7',
    ]);
    expect(headings[0]).toHaveAttribute('aria-current', 'date');
    expect(headings[0]).toHaveClass('bg-day-today');
    expect(headings[1]).not.toHaveAttribute('aria-current');
    expect(headings[1]).toHaveClass('bg-day-tomorrow');
    expect(headings[2]).toHaveClass('bg-day-later');
  });

  it('shows days before today in muted colors', async () => {
    renderList(withLate, {}, '2026-10-27T10:00:00Z');

    const monday = await screen.findByRole('heading', { level: 2, name: 'Monday, October 26' });
    expect(monday).toHaveClass('bg-muted');
    expect(
      screen.getByRole('heading', { level: 2, name: 'Today, Tuesday, October 27' }),
    ).toBeInTheDocument();
  });

  it('counts time from the server clock as of the response', async () => {
    // The server said Friday 10:00 three days ago by the browser's clock: now it is Monday 10:00.
    renderAtSchedule(
      <ScheduleList schedule={withLate} receivedAt={Date.now() - 3 * 24 * 60 * 60 * 1000} />,
    );

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Today, Monday, October 26' }),
    ).toBeInTheDocument();
  });

  it('mutes lessons that have ended', async () => {
    // Monday 19:45 in Berlin: Ballet (18:00–19:30) is over, Contemporary on Tuesday is not.
    renderList(schedule, {}, '2026-10-26T18:45:00Z');

    expect(
      await screen.findByRole('button', { name: 'Ballet, Monday, October 26, 18:00–19:30' }),
    ).toHaveAttribute('data-past');
    expect(
      screen.getByRole('button', {
        name: 'Contemporary, Tuesday, October 27, 17:00–18:00, Canceled',
      }),
    ).not.toHaveAttribute('data-past');
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    renderList(schedule);

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Montag, 26. Oktober' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Abgesagt')).toBeInTheDocument();
    expect(screen.getByText('Anfänger')).toBeInTheDocument();
  });

  it('asks a guest for no family, whose 401 would sign out', async () => {
    const get = mockApiGet(signedOut);
    renderList(schedule);

    await screen.findByRole('button', { name: /^Ballet/ });
    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/auth/me', expect.anything()));
    expect(get).not.toHaveBeenCalledWith('/api/v1/family', expect.anything());
  });

  it("renders the theme's day headings and cards, with the quick action", async () => {
    mockApiGet({
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/family': { body: { ...family, dependents: [] } },
    });
    const theme: Theme = {
      id: 'werkstatt',
      slots: {
        DayHeading: ({ id, date, prevId, nextId }: DayHeadingProps) => (
          <h2 id={id} data-prev={prevId} data-next={nextId}>
            Day {date}
          </h2>
        ),
        LessonCard: ({ lesson, quickAction, onOpen }: LessonCardProps) => (
          <button type="button" onClick={onOpen}>
            {lesson.courseType?.name}: {quickAction.kind}
          </button>
        ),
      },
    };
    renderAtSchedule(
      <ThemeContext.Provider value={theme}>
        <ScheduleList schedule={schedule} receivedAt={Date.now()} />
      </ThemeContext.Provider>,
    );

    // The region is still named by its (the theme's) heading.
    const monday = within(await screen.findByRole('region', { name: 'Day 2026-10-26' }));
    expect(await monday.findByRole('button', { name: 'Ballet: book' })).toBeInTheDocument();
    expect(
      day('Day 2026-10-27').getByRole('button', { name: 'Contemporary: none' }),
    ).toBeInTheDocument();
    const headings = screen.getAllByRole('heading', { level: 2 });
    expect(headings.map((h) => [h.dataset.prev, h.dataset.next])).toEqual([
      [undefined, headings[1]!.id],
      [headings[0]!.id, headings[2]!.id],
      [headings[1]!.id, undefined],
    ]);
  });
});
