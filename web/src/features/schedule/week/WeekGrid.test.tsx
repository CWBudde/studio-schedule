import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import type { LessonSummary, Schedule } from '@/api/client';
import i18n from '@/i18n';
import { ids, lessonBallet, lessonContemporary, schedule } from '@/test/mockApi';
import { todayIn } from '@/lib/schoolTime';
import { renderAtSchedule } from '@/test/utils';
import { WeekGrid } from './WeekGrid';

/** The lesson block of a day column, found by the column's heading. */
async function blockIn(day: string, courseType: RegExp) {
  const column = await screen.findByRole('region', { name: day });
  return within(column).getByRole('button', { name: courseType });
}

function lessonAt(id: number, name: string, startsAt: string, minutes = 60): LessonSummary {
  return {
    ...lessonBallet,
    id: `0190a000-0000-7000-8000-${String(id).padStart(12, '0')}`,
    courseType: { id: ids.ballet, name },
    startsAt,
    endsAt: new Date(Date.parse(startsAt) + minutes * 60_000).toISOString(),
  };
}

describe('WeekGrid', () => {
  it('places lessons by school date and wall-clock time in the school zone', async () => {
    renderAtSchedule(
      <WeekGrid
        schedule={schedule}
        today={todayIn('Europe/Berlin', schedule.now)}
        weekStart="2026-10-26"
      />,
      {
        from: '2026-10-26',
      },
    );

    expect(await screen.findByRole('heading', { level: 2 })).toHaveTextContent(
      'Oct 26 – Nov 1, 2026',
    );

    const ballet = await blockIn('Mon 26', /Ballet/);
    expect(ballet).toHaveAccessibleName('Ballet, Monday, October 26, 18:00–19:30, Room A');
    expect(ballet).toHaveAttribute('data-start', String(18 * 60));
    expect(ballet).toHaveTextContent('18:00–19:30');

    const contemporary = await blockIn('Tue 27', /Contemporary/);
    expect(contemporary).toHaveAccessibleName(
      'Contemporary, Tuesday, October 27, 17:00–18:00, Room B, Canceled',
    );
    expect(within(contemporary).getByText('Canceled')).toBeInTheDocument();

    // The axis runs from the earliest start to the latest end, in whole hours.
    for (const hour of ['17:00', '18:00', '19:00', '20:00']) {
      expect(screen.getByText(hour)).toBeInTheDocument();
    }
    expect(screen.queryByText('16:00')).not.toBeInTheDocument();
    expect(screen.queryByText(/Nothing is published/)).not.toBeInTheDocument();
  });

  it('keeps 18:00 at 18:00 in the weeks the clocks change', async () => {
    const autumn: Schedule = {
      ...schedule,
      from: '2026-10-19',
      to: '2026-10-26',
      lessons: [
        lessonAt(901, 'Saturday class', '2026-10-24T16:00:00Z'),
        lessonAt(902, 'Sunday class', '2026-10-25T17:00:00Z'),
      ],
    };
    renderAtSchedule(
      <WeekGrid
        schedule={autumn}
        today={todayIn('Europe/Berlin', autumn.now)}
        weekStart="2026-10-19"
      />,
    );

    expect(await blockIn('Sat 24', /Saturday class/)).toHaveAttribute('data-start', '1080');
    expect(await blockIn('Sun 25', /Sunday class/)).toHaveAttribute('data-start', '1080');
  });

  it('keeps 18:00 at 18:00 when the clocks go forward', async () => {
    const spring: Schedule = {
      ...schedule,
      from: '2027-03-22',
      to: '2027-03-29',
      lessons: [
        lessonAt(903, 'Saturday class', '2027-03-27T17:00:00Z'),
        lessonAt(904, 'Sunday class', '2027-03-28T16:00:00Z'),
      ],
    };
    renderAtSchedule(
      <WeekGrid
        schedule={spring}
        today={todayIn('Europe/Berlin', spring.now)}
        weekStart="2027-03-22"
      />,
    );

    expect(await blockIn('Sat 27', /Saturday class/)).toHaveAttribute('data-start', '1080');
    expect(await blockIn('Sun 28', /Sunday class/)).toHaveAttribute('data-start', '1080');
  });

  it('puts a lesson at 00:30 in Berlin on the next day', async () => {
    const late: Schedule = {
      ...schedule,
      lessons: [lessonAt(905, 'Night class', '2026-10-26T23:30:00Z')],
    };
    renderAtSchedule(
      <WeekGrid
        schedule={late}
        today={todayIn('Europe/Berlin', late.now)}
        weekStart="2026-10-26"
      />,
    );

    const block = await blockIn('Tue 27', /Night class/);
    expect(block).toHaveAttribute('data-start', '30');
    expect(within(screen.getByRole('region', { name: 'Mon 26' })).queryByRole('button')).toBeNull();
  });

  it('sets overlapping lessons side by side', async () => {
    const busy: Schedule = {
      ...schedule,
      lessons: [
        lessonAt(906, 'Early', '2026-10-26T16:00:00Z', 90),
        lessonAt(907, 'Overlapping', '2026-10-26T17:00:00Z'),
        lessonAt(908, 'Later', '2026-10-26T19:00:00Z'),
      ],
    };
    renderAtSchedule(
      <WeekGrid
        schedule={busy}
        today={todayIn('Europe/Berlin', busy.now)}
        weekStart="2026-10-26"
      />,
    );

    const lanes = async (name: RegExp) => {
      const block = await blockIn('Mon 26', name);
      return [block.dataset.lane, block.dataset.lanes];
    };
    expect(await lanes(/Early/)).toEqual(['0', '2']);
    expect(await lanes(/Overlapping/)).toEqual(['1', '2']);
    expect(await lanes(/Later/)).toEqual(['0', '1']);
  });

  it('marks workshops', async () => {
    const workshopWeek: Schedule = { ...schedule, from: '2026-11-02', to: '2026-11-09' };
    renderAtSchedule(
      <WeekGrid
        schedule={workshopWeek}
        today={todayIn('Europe/Berlin', workshopWeek.now)}
        weekStart="2026-11-02"
      />,
    );

    const block = await blockIn('Sat 7', /Jazz/);
    expect(within(block).getByText('Workshop')).toBeInTheDocument();
    expect(block).toHaveAccessibleName(/, Workshop$/);
  });

  it('navigates between weeks via the search params', async () => {
    const user = userEvent.setup();
    const { router } = renderAtSchedule(
      <WeekGrid
        schedule={schedule}
        today={todayIn('Europe/Berlin', schedule.now)}
        weekStart="2026-10-26"
      />,
      {
        from: '2026-10-26',
        lesson: ids.lessonBallet,
      },
    );

    await user.click(await screen.findByRole('button', { name: 'Next week' }));
    expect(router.state.location.search).toEqual({ from: '2026-11-02', lesson: ids.lessonBallet });

    await user.click(screen.getByRole('button', { name: 'Previous week' }));
    expect(router.state.location.search).toEqual({ from: '2026-10-19', lesson: ids.lessonBallet });

    await user.click(screen.getByRole('button', { name: 'This week' }));
    expect(router.state.location.search).toEqual({ lesson: ids.lessonBallet });
  });

  it('disables "This week" in the current week and highlights today', async () => {
    // The schedule was computed on Friday 2026-10-23.
    renderAtSchedule(
      <WeekGrid
        schedule={{ ...schedule, lessons: [] }}
        today="2026-10-23"
        weekStart="2026-10-19"
      />,
    );

    expect(await screen.findByRole('button', { name: 'This week' })).toBeDisabled();
    expect(screen.getByRole('heading', { level: 3, name: 'Fri 23' })).toHaveAttribute(
      'aria-current',
      'date',
    );
    expect(screen.getByRole('heading', { level: 3, name: 'Thu 22' })).not.toHaveAttribute(
      'aria-current',
    );
  });

  it('opens a lesson by writing its id to the search params', async () => {
    const user = userEvent.setup();
    const { router } = renderAtSchedule(
      <WeekGrid
        schedule={schedule}
        today={todayIn('Europe/Berlin', schedule.now)}
        weekStart="2026-10-26"
      />,
      {
        from: '2026-10-26',
      },
    );

    await user.click(await blockIn('Mon 26', /Ballet/));
    expect(router.state.location.search).toEqual({ from: '2026-10-26', lesson: ids.lessonBallet });

    // Canceled lessons stay visible and open too.
    await user.click(await blockIn('Tue 27', /Contemporary/));
    expect(router.state.location.search).toEqual({
      from: '2026-10-26',
      lesson: lessonContemporary.id,
    });
  });

  it('renders the empty week with a note', async () => {
    renderAtSchedule(
      <WeekGrid
        schedule={{ ...schedule, lessons: [] }}
        today="2026-10-23"
        weekStart="2026-10-26"
      />,
    );

    expect(await screen.findByText('Nothing is published for this week.')).toBeInTheDocument();
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Mon 26',
      'Tue 27',
      'Wed 28',
      'Thu 29',
      'Fri 30',
      'Sat 31',
      'Sun 1',
    ]);
    expect(screen.getByText('08:00')).toBeInTheDocument();
    expect(screen.getByText('22:00')).toBeInTheDocument();
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    renderAtSchedule(
      <WeekGrid
        schedule={schedule}
        today={todayIn('Europe/Berlin', schedule.now)}
        weekStart="2026-10-26"
      />,
    );

    expect(await screen.findByRole('heading', { level: 2 })).toHaveTextContent(
      '26. Okt. – 1. Nov. 2026',
    );
    expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
      'Mo 26',
      'Di 27',
      'Mi 28',
      'Do 29',
      'Fr 30',
      'Sa 31',
      'So 1',
    ]);
    expect(screen.getByRole('button', { name: 'Nächste Woche' })).toBeInTheDocument();
    expect(await blockIn('Mo 26', /Ballet/)).toHaveAccessibleName(
      'Ballet, Montag, 26. Oktober, 18:00–19:30, Room A',
    );
    expect(within(await blockIn('Di 27', /Contemporary/)).getByText('Abgesagt')).toBeVisible();
  });
});
