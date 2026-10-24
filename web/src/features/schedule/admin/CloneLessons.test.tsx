import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent, { type UserEvent } from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '@/api/client';
import type { LessonClone, ScheduleParams } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import {
  catalogFixtures,
  ids,
  lessonBallet,
  lessonWorkshop,
  meMember,
  meOffice,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  problem,
  publicSettings,
  schedule,
  staffEntries,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import type { ScheduleSearch } from '../search';
import { SchedulePage } from '../SchedulePage';

// Friday 2026-10-23 in Berlin: the earliest target is today for a day and
// Monday 2026-10-26 for a week.
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-23T10:00:00Z'));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

/** Mocks the API for `me` and records the query of every schedule request. */
function mockSchedule(me: Me = meOffice) {
  const requests: ScheduleParams[] = [];
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/auth/me': { body: me },
    '/api/v1/admin/staff': { body: { items: staffEntries } },
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/schedule': (init: MockRequest) => {
      requests.push(init.params?.query as ScheduleParams);
      return { body: schedule };
    },
  });
  return requests;
}

/** A Saturday workshop (no class) at 18:00–19:30 in Berlin, as a source lesson. */
const lessonSaturday = {
  ...lessonWorkshop,
  id: ids.lessonContemporary,
  startsAt: '2026-10-31T17:00:00Z',
  endsAt: '2026-10-31T18:30:00Z',
};

/** The ballet lesson of Monday 2026-10-26 copied to Monday 2026-11-02, same time. */
const balletCopy = {
  source: lessonBallet,
  startsAt: '2026-11-02T17:00:00Z',
  endsAt: '2026-11-02T18:30:00Z',
};

const previewOfDay: LessonClone = { created: [balletCopy], skipped: [] };

/** Renders the schedule, opens the clone sheet and returns it. */
async function openClone(user: UserEvent, search: ScheduleSearch = { from: '2026-10-26' }) {
  render(<Toaster />);
  const rendered = renderAtSchedule(<SchedulePage />, search);
  await user.click(await screen.findByRole('button', { name: 'Clone…' }));
  const sheet = within(await screen.findByRole('dialog', { name: 'Clone lessons' }));
  return { ...rendered, sheet };
}

/** Replaces the date of the input `label` of `scope`. */
async function fill(
  user: UserEvent,
  scope: ReturnType<typeof within>,
  label: string,
  date: string,
) {
  const input = scope.getByLabelText(label);
  await user.clear(input);
  await user.type(input, date);
}

describe('CloneLessons', () => {
  it('is offered to the office only', async () => {
    mockSchedule(meMember);
    renderAtSchedule(<SchedulePage />, { from: '2026-10-26' });

    expect(await screen.findByRole('heading', { name: 'Monday, October 26' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Clone…' })).not.toBeInTheDocument();
  });

  it('previews a day from the shown one with every skip reason', async () => {
    mockSchedule();
    const post = mockApiPost({
      '/api/v1/admin/lessons/clone/preview': {
        body: {
          created: [balletCopy],
          skipped: [
            {
              source: { ...lessonBallet, id: ids.lessonContemporary },
              startsAt: '2026-11-02T16:00:00Z',
              endsAt: '2026-11-02T17:00:00Z',
              reason: 'class-slot',
            },
            {
              source: lessonSaturday,
              startsAt: '2026-11-07T17:00:00Z',
              endsAt: '2026-11-07T18:30:00Z',
              reason: 'started',
            },
            {
              source: { ...lessonWorkshop, id: id(900) },
              startsAt: '2026-11-02T15:00:00Z',
              endsAt: '2026-11-02T16:00:00Z',
              reason: 'staff-inactive',
            },
            {
              source: { ...lessonWorkshop, id: id(901) },
              startsAt: '2026-11-02T19:00:00Z',
              endsAt: '2026-11-02T20:00:00Z',
              reason: 'room-taken',
              lesson: {
                id: id(902),
                courseType: { id: ids.contemporary, name: 'Contemporary' },
                startsAt: '2026-11-02T18:30:00Z',
                endsAt: '2026-11-02T19:30:00Z',
              },
            },
            {
              source: { ...lessonWorkshop, id: id(903) },
              startsAt: '2026-11-02T17:30:00Z',
              endsAt: '2026-11-02T18:00:00Z',
              reason: 'room-taken',
              lesson: {
                copyOf: ids.lessonBallet,
                courseType: { id: ids.ballet, name: 'Ballet' },
                startsAt: balletCopy.startsAt,
                endsAt: balletCopy.endsAt,
              },
            },
          ],
        } satisfies LessonClone,
      },
    });
    const user = userEvent.setup();
    const { sheet } = await openClone(user);

    expect(sheet.getByRole('radio', { name: 'One day' })).toHaveAttribute('aria-checked', 'true');
    expect(sheet.getByLabelText('Copy from')).toHaveValue('2026-10-26');
    expect(sheet.getByLabelText('Copy from')).toHaveAccessibleDescription('Monday, October 26');
    expect(sheet.getByLabelText('Copy to')).toHaveValue('2026-11-02');
    await user.click(sheet.getByRole('button', { name: 'Preview' }));

    const preview = within(await sheet.findByRole('region', { name: 'Preview' }));
    expect(post).toHaveBeenCalledWith('/api/v1/admin/lessons/clone/preview', {
      body: { unit: 'day', source: '2026-10-26', target: '2026-11-02' },
    });
    // Times in the school's zone (Berlin), not the browser's.
    const created = within(preview.getByRole('list', { name: 'Will be created: 1 lesson' }));
    const copy = created.getByRole('listitem');
    expect(copy).toHaveTextContent('Ballet');
    expect(copy).toHaveTextContent('Monday, November 2, 18:00–19:30');
    expect(copy).toHaveTextContent('North Studio · Room A · Alex Example, Sam Pianist');
    expect(copy).toHaveTextContent('Copy of Monday, October 26');

    const skipped = within(preview.getByRole('list', { name: 'Skipped: 5 lessons' }));
    const [slot, started, inactive, taken, byCopy] = skipped.getAllByRole('listitem');
    expect(slot).toHaveTextContent('Its class already has this lesson in the target week.');
    expect(started).toHaveTextContent('The copy would start at a time that has passed.');
    expect(inactive).toHaveTextContent('Its instructor or accompanist is no longer active.');
    expect(taken).toHaveTextContent(
      'Room A is taken: Contemporary, Monday, November 2, 19:30–20:30 uses the room.',
    );
    expect(byCopy).toHaveTextContent(
      'Room A is taken by another copy in this clone, the copy of Ballet, Monday, October 26, 18:00–19:30.',
    );
    expect(preview.getByRole('button', { name: 'Clone 1 lesson' })).toBeEnabled();
  });

  it('suggests the shown week and accepts only Mondays', async () => {
    mockSchedule();
    const post = mockApiPost({ '/api/v1/admin/lessons/clone/preview': { body: previewOfDay } });
    const user = userEvent.setup();
    const { sheet } = await openClone(user, { view: 'week', from: '2026-10-28' });

    expect(sheet.getByRole('radio', { name: 'One week' })).toHaveAttribute('aria-checked', 'true');
    expect(sheet.getByLabelText('Copy from')).toHaveValue('2026-10-26');
    expect(sheet.getByLabelText('Copy from')).toHaveAccessibleDescription(
      'Monday, October 26 to Sunday, November 1',
    );
    expect(sheet.getByLabelText('Copy to')).toHaveValue('2026-11-02');

    await fill(user, sheet, 'Copy from', '2026-10-28');
    await fill(user, sheet, 'Copy to', '2026-10-26');
    await user.click(sheet.getByRole('button', { name: 'Preview' }));

    expect(await sheet.findByText('Choose a Monday.')).toBeInTheDocument();
    expect(sheet.getByLabelText('Copy from')).toHaveAccessibleDescription(
      'A Monday: the week runs from Monday to Sunday. Choose a Monday.',
    );

    // A Monday as the source; the target is the current week, which has begun.
    await fill(user, sheet, 'Copy from', '2026-10-19');
    await fill(user, sheet, 'Copy to', '2026-10-19');
    await user.click(sheet.getByRole('button', { name: 'Preview' }));
    expect(
      await sheet.findByText('Choose another date than the one to copy from.'),
    ).toBeInTheDocument();
    await fill(user, sheet, 'Copy to', '2026-10-12');
    expect(
      await sheet.findByText('This week has begun already; choose a later one.'),
    ).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('snaps the dates to Mondays when switching to a week', async () => {
    mockSchedule();
    const user = userEvent.setup();
    const { sheet } = await openClone(user, { from: '2026-10-28' });

    expect(sheet.getByLabelText('Copy from')).toHaveValue('2026-10-28');
    expect(sheet.getByLabelText('Copy to')).toHaveValue('2026-11-04');
    await user.click(sheet.getByRole('radio', { name: 'One week' }));

    expect(sheet.getByLabelText('Copy from')).toHaveValue('2026-10-26');
    expect(sheet.getByLabelText('Copy to')).toHaveValue('2026-11-02');
  });

  it('shows a refusal of the API on its field', async () => {
    mockSchedule();
    mockApiPost({
      '/api/v1/admin/lessons/clone/preview': problem(422, 'validation failed', [
        { location: 'body.target', message: 'the target must not lie in the past' },
      ]),
    });
    const user = userEvent.setup();
    const { sheet } = await openClone(user);

    await user.click(sheet.getByRole('button', { name: 'Preview' }));

    expect(await sheet.findByText('Please check your entries.')).toBeInTheDocument();
    expect(sheet.getByLabelText('Copy to')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Copy to')).toHaveAccessibleDescription(
      'Monday, November 2 Please check this entry.',
    );
    expect(sheet.getByLabelText('Copy from')).not.toHaveAttribute('aria-invalid');
    expect(sheet.queryByRole('region', { name: 'Preview' })).not.toBeInTheDocument();
  });

  it('clones, shows the result of the clone and refreshes the schedule', async () => {
    const requests = mockSchedule();
    const post = mockApiPost({
      '/api/v1/admin/lessons/clone/preview': {
        body: { created: [balletCopy, { ...balletCopy, source: lessonSaturday }], skipped: [] },
      },
      // Meanwhile the room of the second copy was taken.
      '/api/v1/admin/lessons/clone': {
        body: {
          created: [{ ...balletCopy, id: id(911) }],
          skipped: [
            {
              source: lessonSaturday,
              startsAt: balletCopy.startsAt,
              endsAt: balletCopy.endsAt,
              reason: 'room-taken',
            },
          ],
        } satisfies LessonClone,
      },
    });
    const user = userEvent.setup();
    const { sheet } = await openClone(user);
    const shown = () => requests.filter((r) => r.from === '2026-10-26' && r.to === '2026-11-09');
    await waitFor(() => expect(shown()).toHaveLength(1));

    await user.click(sheet.getByRole('button', { name: 'Preview' }));
    await user.click(await sheet.findByRole('button', { name: 'Clone 2 lessons' }));

    const result = within(await sheet.findByRole('region', { name: 'Result' }));
    expect(post).toHaveBeenLastCalledWith('/api/v1/admin/lessons/clone', {
      body: { unit: 'day', source: '2026-10-26', target: '2026-11-02' },
    });
    expect(
      result.getByText('Something changed since the preview, so the result differs from it.'),
    ).toBeInTheDocument();
    expect(
      within(result.getByRole('list', { name: 'Created: 1 lesson' })).getByRole('listitem'),
    ).toHaveTextContent('Ballet');
    expect(
      within(result.getByRole('list', { name: 'Skipped: 1 lesson' })).getByRole('listitem'),
    ).toHaveTextContent('Room A is taken at that time.');
    expect(sheet.queryByRole('region', { name: 'Preview' })).not.toBeInTheDocument();
    expect(await screen.findByText('1 lesson has been cloned.')).toBeInTheDocument();
    // The schedule is asked again, so the copies show.
    await waitFor(() => expect(shown()).toHaveLength(2));

    await user.click(result.getByRole('button', { name: 'Close' }));
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Clone lessons' })).not.toBeInTheDocument(),
    );
  });

  it('finds no difference when the room-taking copy only got its id', async () => {
    mockSchedule();
    /** The skips of a day: one room taken by another copy, one by a class lesson not generated yet. */
    const skipped = (copyId?: string): LessonClone['skipped'] => [
      {
        source: { ...lessonWorkshop, id: id(903) },
        startsAt: '2026-11-02T17:30:00Z',
        endsAt: '2026-11-02T18:00:00Z',
        reason: 'room-taken',
        lesson: {
          id: copyId,
          copyOf: ids.lessonBallet,
          courseType: { id: ids.ballet, name: 'Ballet' },
          startsAt: balletCopy.startsAt,
          endsAt: balletCopy.endsAt,
        },
      },
      {
        source: { ...lessonWorkshop, id: id(904) },
        startsAt: '2026-11-02T19:00:00Z',
        endsAt: '2026-11-02T20:00:00Z',
        reason: 'room-taken',
        lesson: {
          courseType: { id: ids.contemporary, name: 'Contemporary' },
          startsAt: '2026-11-02T18:30:00Z',
          endsAt: '2026-11-02T19:30:00Z',
        },
      },
    ];
    mockApiPost({
      '/api/v1/admin/lessons/clone/preview': {
        body: { created: [balletCopy], skipped: skipped() } satisfies LessonClone,
      },
      '/api/v1/admin/lessons/clone': {
        body: {
          created: [{ ...balletCopy, id: id(911) }],
          skipped: skipped(id(911)),
        } satisfies LessonClone,
      },
    });
    const user = userEvent.setup();
    const { sheet } = await openClone(user);

    await user.click(sheet.getByRole('button', { name: 'Preview' }));
    await user.click(await sheet.findByRole('button', { name: 'Clone 1 lesson' }));

    const result = within(await sheet.findByRole('region', { name: 'Result' }));
    expect(result.queryByText(/Something changed since the preview/)).not.toBeInTheDocument();
    const [byCopy, byClass] = within(
      result.getByRole('list', { name: 'Skipped: 2 lessons' }),
    ).getAllByRole('listitem');
    expect(byCopy).toHaveTextContent(
      'Room A is taken by another copy in this clone, the copy of Ballet, Monday, October 26, 18:00–19:30.',
    );
    expect(byClass).toHaveTextContent(
      'Room A is taken: Contemporary, Monday, November 2, 19:30–20:30 uses the room.',
    );
  });

  it('cannot clone when nothing would be created', async () => {
    mockSchedule();
    mockApiPost({
      '/api/v1/admin/lessons/clone/preview': {
        body: {
          created: [],
          skipped: [{ ...balletCopy, reason: 'class-slot' }],
        } satisfies LessonClone,
      },
    });
    const user = userEvent.setup();
    const { sheet } = await openClone(user);

    await user.click(sheet.getByRole('button', { name: 'Preview' }));

    const preview = within(await sheet.findByRole('region', { name: 'Preview' }));
    expect(preview.getByText('Nothing would be created.')).toBeInTheDocument();
    expect(preview.getByRole('button', { name: 'Clone 0 lessons' })).toBeDisabled();
  });

  it('says so when there is nothing to copy', async () => {
    mockSchedule();
    mockApiPost({
      '/api/v1/admin/lessons/clone/preview': { body: { created: [], skipped: [] } },
    });
    const user = userEvent.setup();
    const { sheet } = await openClone(user);

    await user.click(sheet.getByRole('button', { name: 'Preview' }));

    const preview = within(await sheet.findByRole('region', { name: 'Preview' }));
    expect(preview.getByText('There are no scheduled lessons to copy here.')).toBeInTheDocument();
    expect(preview.getByRole('button', { name: 'Clone 0 lessons' })).toBeDisabled();
  });

  it('discards the preview when an input changes', async () => {
    mockSchedule();
    mockApiPost({ '/api/v1/admin/lessons/clone/preview': { body: previewOfDay } });
    const user = userEvent.setup();
    const { sheet } = await openClone(user);

    await user.click(sheet.getByRole('button', { name: 'Preview' }));
    expect(await sheet.findByRole('region', { name: 'Preview' })).toBeInTheDocument();

    await fill(user, sheet, 'Copy to', '2026-11-03');

    expect(sheet.queryByRole('region', { name: 'Preview' })).not.toBeInTheDocument();
    expect(sheet.queryByRole('button', { name: /^Clone \d/ })).not.toBeInTheDocument();
  });
});

/** A fictional UUIDv7 for lessons the fixtures do not have. */
function id(n: number) {
  return `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;
}
