import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { LessonSummary, Schedule } from '@/api/client';
import i18n from '@/i18n';
import { ids, lessonBallet, lessonContemporary, lessonWorkshop, schedule } from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import type { ScheduleSearch } from '../search';
import { ScheduleList } from './ScheduleList';

/** Ballet at 00:30–01:30 Berlin on Tuesday 2026-10-27 (still Monday evening in New York and UTC). */
const lessonLate: LessonSummary = {
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

describe('ScheduleList', () => {
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
});
