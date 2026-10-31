import { render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ScheduleLesson } from '@/api/client';
import { lessonBallet } from '@/test/mockApi';
import type { DayHeadingProps, ScheduleDay, ScheduleListProps, Theme } from '../slots';
import { ThemeContext } from '../themeContext';
import { roomColumns } from './roomColumns';
import { ScheduleList } from './ScheduleList';

const north = { id: '0190a000-0000-7000-8000-00000000a001', name: 'Nordhaus' };
const south = { id: '0190a000-0000-7000-8000-00000000a002', name: 'Südhaus' };
const oben = { id: '0190a000-0000-7000-8000-00000000b001', name: 'Oben' };
const unten = { id: '0190a000-0000-7000-8000-00000000b002', name: 'Unten' };
const keller = { id: '0190a000-0000-7000-8000-00000000b003', name: 'Keller' };
const garten = { id: '0190a000-0000-7000-8000-00000000b004', name: 'Garten' };

/** A lesson named `name`, in `room` at `location`, starting at `startsAt`. */
function lesson(
  name: string,
  room: ScheduleLesson['room'],
  startsAt: string,
  location: ScheduleLesson['location'] = north,
): ScheduleLesson {
  return {
    ...lessonBallet,
    id: name,
    courseType: { id: lessonBallet.courseType.id, name },
    room,
    location,
    startsAt,
    endsAt: new Date(Date.parse(startsAt) + 60 * 60_000).toISOString(),
  };
}

function day(date: string, lessons: ScheduleLesson[]): ScheduleDay {
  return { date, tone: 'later', headingId: `day-${date}`, lessons };
}

const renderLesson = (l: ScheduleLesson) => <p>{l.courseType.name}</p>;

function renderList(days: ScheduleDay[], props: Partial<ScheduleListProps> = {}) {
  return render(
    <ScheduleList
      days={days}
      timeZone="Europe/Berlin"
      now={Date.parse('2026-10-20T10:00:00Z')}
      onOpen={() => undefined}
      renderLesson={renderLesson}
      {...props}
    />,
  );
}

/** Makes the wide layout's media query match. */
function onWide() {
  vi.spyOn(window, 'matchMedia').mockImplementation(
    (query: string) =>
      ({
        matches: query.includes('min-width: 768px'),
        media: query,
        onchange: null,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }) as MediaQueryList,
  );
}

/** The region of a day, found by its heading's id. */
function dayRegion(date: string) {
  const heading = document.getElementById(`day-${date}`);
  const section = heading?.closest('section');
  if (!section) throw new Error(`no section for ${date}`);
  return within(section);
}

/** The lesson names in a list, in order. */
function names(list: HTMLElement) {
  return within(list)
    .getAllByRole('listitem')
    .map((item) => item.textContent);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('Werkstatt ScheduleList, wide', () => {
  it('puts each lesson into the column of its room, under the room header', () => {
    onWide();
    renderList([
      day('2026-10-26', [
        lesson('Ballet', oben, '2026-10-26T16:00:00Z'),
        lesson('Jazz', unten, '2026-10-26T16:00:00Z'),
        lesson('Tap', oben, '2026-10-26T17:00:00Z'),
      ]),
    ]);

    const region = dayRegion('2026-10-26');
    expect(region.getAllByRole('list')).toHaveLength(2);
    expect(names(region.getByRole('list', { name: 'Oben' }))).toEqual(['Ballet', 'Tap']);
    expect(names(region.getByRole('list', { name: 'Unten' }))).toEqual(['Jazz']);
    expect(region.getByText('Oben')).toBeInTheDocument();
    expect(region.getByText('Unten')).toBeInTheDocument();
  });

  it('shows one full-width column for a day with one room', () => {
    onWide();
    renderList([
      day('2026-10-26', [
        lesson('Ballet', oben, '2026-10-26T16:00:00Z'),
        lesson('Tap', oben, '2026-10-26T17:00:00Z'),
      ]),
    ]);

    const lists = dayRegion('2026-10-26').getAllByRole('list');
    expect(lists).toHaveLength(1);
    expect(lists[0]).toHaveAccessibleName('Oben');
    expect(names(lists[0]!)).toEqual(['Ballet', 'Tap']);
  });

  it("shows only the rooms a day uses, each with that day's lessons", () => {
    onWide();
    renderList([
      day('2026-10-26', [
        lesson('Ballet', oben, '2026-10-26T16:00:00Z'),
        lesson('Jazz', unten, '2026-10-26T17:00:00Z'),
      ]),
      day('2026-10-27', [
        lesson('Tap', oben, '2026-10-27T16:00:00Z'),
        lesson('Hip-hop', oben, '2026-10-27T17:00:00Z'),
        lesson('Stretching', keller, '2026-10-27T18:00:00Z'),
      ]),
    ]);

    const monday = dayRegion('2026-10-26');
    expect(monday.queryByRole('list', { name: 'Keller' })).not.toBeInTheDocument();
    expect(monday.getAllByRole('list')).toHaveLength(2);

    const tuesday = dayRegion('2026-10-27');
    expect(tuesday.queryByRole('list', { name: 'Unten' })).not.toBeInTheDocument();
    expect(tuesday.queryByText('Unten')).not.toBeInTheDocument();
    expect(tuesday.getAllByRole('list')).toHaveLength(2);
    expect(names(tuesday.getByRole('list', { name: 'Keller' }))).toEqual(['Stretching']);
  });

  it('orders a column by start time and the columns by location, then room', () => {
    onWide();
    renderList([
      day('2026-10-26', [
        lesson('Late', oben, '2026-10-26T18:00:00Z'),
        lesson('Early', oben, '2026-10-26T15:00:00Z'),
        lesson('Middle', oben, '2026-10-26T16:00:00Z'),
        lesson('Same time, first', unten, '2026-10-26T16:00:00Z'),
        lesson('Same time, second', unten, '2026-10-26T16:00:00Z'),
      ]),
    ]);

    const region = dayRegion('2026-10-26');
    expect(names(region.getByRole('list', { name: 'Oben' }))).toEqual(['Early', 'Middle', 'Late']);
    expect(names(region.getByRole('list', { name: 'Unten' }))).toEqual([
      'Same time, first',
      'Same time, second',
    ]);
    expect(region.getAllByRole('list').map((list) => list.getAttribute('aria-labelledby'))).toEqual(
      [region.getByText('Oben').id, region.getByText('Unten').id],
    );
  });

  it('names the location in the headers when a day spans two', () => {
    onWide();
    renderList([
      day('2026-10-26', [
        lesson('Ballet', oben, '2026-10-26T16:00:00Z', south),
        lesson('Jazz', garten, '2026-10-26T16:00:00Z', north),
        lesson('Tap', unten, '2026-10-26T17:00:00Z', north),
      ]),
    ]);

    const lists = dayRegion('2026-10-26').getAllByRole('list');
    expect(lists.map((list) => list.getAttribute('aria-labelledby'))).toHaveLength(3);
    expect(lists[0]).toHaveAccessibleName('Garten · Nordhaus');
    expect(lists[1]).toHaveAccessibleName('Unten · Nordhaus');
    expect(lists[2]).toHaveAccessibleName('Oben · Südhaus');
  });

  it('renders every lesson exactly once', () => {
    onWide();
    const spy = vi.fn(renderLesson);
    const lessons = [
      lesson('Ballet', oben, '2026-10-26T16:00:00Z'),
      lesson('Jazz', unten, '2026-10-26T16:00:00Z'),
      lesson('Tap', keller, '2026-10-26T17:00:00Z'),
    ];
    renderList([day('2026-10-26', lessons)], { renderLesson: spy });

    expect(spy.mock.calls.map(([l]) => l.id).sort()).toEqual(['Ballet', 'Jazz', 'Tap']);
    expect(screen.getAllByText(/^(Ballet|Jazz|Tap)$/)).toHaveLength(3);
  });
});

describe('Werkstatt ScheduleList, narrow', () => {
  it('shows one list of all lessons in the given order, without room headers', () => {
    const spy = vi.fn(renderLesson);
    renderList(
      [
        day('2026-10-26', [
          lesson('Jazz', unten, '2026-10-26T16:00:00Z'),
          lesson('Ballet', oben, '2026-10-26T15:00:00Z'),
          lesson('Tap', oben, '2026-10-26T17:00:00Z'),
        ]),
      ],
      { renderLesson: spy },
    );

    const lists = dayRegion('2026-10-26').getAllByRole('list');
    expect(lists).toHaveLength(1);
    expect(lists[0]).not.toHaveAttribute('aria-labelledby');
    expect(names(lists[0]!)).toEqual(['Jazz', 'Ballet', 'Tap']);
    expect(screen.queryByText('Oben')).not.toBeInTheDocument();
    expect(screen.queryByText('Unten')).not.toBeInTheDocument();
    expect(spy.mock.calls.map(([l]) => l.id).sort()).toEqual(['Ballet', 'Jazz', 'Tap']);
  });
});

describe('Werkstatt ScheduleList', () => {
  it('shows `empty` without days', () => {
    renderList([], { empty: <p>Nothing here</p> });
    expect(screen.getByText('Nothing here')).toBeInTheDocument();
  });

  it("gives each day's heading its neighbours' ids", () => {
    const DayHeading = ({ id, prevId, nextId }: DayHeadingProps) => (
      <h2 id={id}>{`${prevId ?? '-'} ${id} ${nextId ?? '-'}`}</h2>
    );
    const theme: Theme = { id: 'werkstatt', slots: { DayHeading } };
    render(
      <ThemeContext.Provider value={theme}>
        <ScheduleList
          days={[
            day('2026-10-26', [lesson('Ballet', oben, '2026-10-26T16:00:00Z')]),
            day('2026-10-27', [lesson('Jazz', oben, '2026-10-27T16:00:00Z')]),
            day('2026-10-28', [lesson('Tap', oben, '2026-10-28T16:00:00Z')]),
          ]}
          timeZone="Europe/Berlin"
          now={0}
          onOpen={() => undefined}
          renderLesson={renderLesson}
        />
      </ThemeContext.Provider>,
    );

    expect(screen.getAllByRole('heading').map((h) => h.textContent)).toEqual([
      '- day-2026-10-26 day-2026-10-27',
      'day-2026-10-26 day-2026-10-27 day-2026-10-28',
      'day-2026-10-27 day-2026-10-28 -',
    ]);
    expect(screen.getByRole('region', { name: /^- day-2026-10-26/ })).toBeInTheDocument();
  });
});

describe('roomColumns', () => {
  it('groups by room, orders by location, room name and id, and keeps start order', () => {
    const twin = { id: '0190a000-0000-7000-8000-00000000b000', name: 'Oben' };
    const columns = roomColumns(
      [
        lesson('B', oben, '2026-10-26T17:00:00Z'),
        lesson('A', oben, '2026-10-26T16:00:00Z'),
        lesson('C', twin, '2026-10-26T16:00:00Z'),
        lesson('D', garten, '2026-10-26T16:00:00Z', south),
      ],
      'de',
    );

    expect(columns.map((c) => [c.key, c.label, c.lessons.map((l) => l.id)])).toEqual([
      [twin.id, 'Oben · Nordhaus', ['C']],
      [oben.id, 'Oben · Nordhaus', ['A', 'B']],
      [garten.id, 'Garten · Südhaus', ['D']],
    ]);
  });

  it('labels by room only within one location', () => {
    const columns = roomColumns([lesson('A', oben, '2026-10-26T16:00:00Z')], 'en');
    expect(columns).toEqual([
      { key: oben.id, room: oben, location: north, label: 'Oben', lessons: [expect.anything()] },
    ]);
  });

  it('returns no columns without lessons', () => {
    expect(roomColumns([], 'en')).toEqual([]);
  });
});
