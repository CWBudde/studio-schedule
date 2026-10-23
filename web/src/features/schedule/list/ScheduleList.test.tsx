import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { LessonSummary, Schedule } from '@/api/client';
import i18n from '@/i18n';
import { ids, lessonBallet, lessonContemporary, lessonWorkshop, schedule } from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
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

function day(name: string) {
  return within(screen.getByRole('region', { name }));
}

describe('ScheduleList', () => {
  it('groups lessons by school day with headings and times in the school time zone', async () => {
    renderAtSchedule(<ScheduleList schedule={withLate} />);

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
    renderAtSchedule(<ScheduleList schedule={schedule} />);

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
    renderAtSchedule(<ScheduleList schedule={schedule} />);

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
    renderAtSchedule(<ScheduleList schedule={restricted} />);

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
    renderAtSchedule(<ScheduleList schedule={schedule} />);

    expect(await screen.findAllByRole('button', { name: /\d{2}:\d{2}–/ })).toHaveLength(3);
    expect(screen.queryByText('Members only')).not.toBeInTheDocument();
    expect(screen.queryByText(/^Hidden/)).not.toBeInTheDocument();
  });

  it('opens the lesson detail by writing the lesson id into the URL', async () => {
    const user = userEvent.setup();
    const { router } = renderAtSchedule(<ScheduleList schedule={schedule} />, {
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
    renderAtSchedule(<ScheduleList schedule={empty} />);

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
    const { router } = renderAtSchedule(<ScheduleList schedule={empty} />, {
      view: 'list',
      level: 'advanced',
      location: ids.locationSouth,
    });

    expect(await screen.findByText('No lessons match the selected filters.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Reset filters' }));

    expect(router.state.location.search).toEqual({ view: 'list' });
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    renderAtSchedule(<ScheduleList schedule={schedule} />);

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Montag, 26. Oktober' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Abgesagt')).toBeInTheDocument();
    expect(screen.getByText('Anfänger')).toBeInTheDocument();
  });
});
