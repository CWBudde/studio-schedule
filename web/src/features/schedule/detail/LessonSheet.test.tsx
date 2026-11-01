import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import {
  ids,
  lessonBalletDetail,
  lessonContemporary,
  lessonWorkshop,
  type MockRequest,
  mockApiGet,
  publicSettings,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { LessonSheet } from './LessonSheet';

afterEach(() => {
  vi.restoreAllMocks();
});

/** A room rental without a course type. */
const rentalId = '0190a000-0000-7000-8000-000000000199';

const details: Record<string, object> = {
  [ids.lessonBallet]: lessonBalletDetail,
  [ids.lessonContemporary]: { ...lessonContemporary, visibility: 'members' },
  [ids.lessonWorkshop]: { ...lessonWorkshop, visibility: 'hidden' },
  [rentalId]: {
    ...lessonWorkshop,
    id: rentalId,
    courseType: undefined,
    kind: 'rental',
    visibility: 'hidden',
    instructor: undefined,
    renter: 'Copperleaf Theater Group',
    capacity: 0,
    freePlaces: 0,
  },
};

function mockLessons() {
  return mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/lessons/{lessonId}': (init: MockRequest) => {
      const lesson = details[init.params?.path?.lessonId ?? ''];
      return lesson ? { body: lesson } : { status: 404, body: { title: 'Not Found' } };
    },
  });
}

describe('LessonSheet', () => {
  it('opens from a deep link and shows the lesson in the school time zone', async () => {
    mockLessons();

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });

    const dialog = await screen.findByRole('dialog', { name: 'Ballet' });
    const sheet = within(dialog);
    expect(sheet.getByText('Monday, October 26')).toBeInTheDocument();
    expect(sheet.getByText('18:00–19:30')).toBeInTheDocument();
    expect(sheet.getByText('Beginner')).toBeInTheDocument();
    expect(sheet.getByText('Room A')).toBeInTheDocument();
    expect(sheet.getByText('North Studio')).toBeInTheDocument();
    expect(sheet.getByText('1 Example Street, Sampletown')).toBeInTheDocument();
    expect(sheet.getByText('Alex Example')).toBeInTheDocument();
    expect(sheet.getByText('Sam Pianist')).toBeInTheDocument();
    expect(sheet.getByText('12 places')).toBeInTheDocument();
    expect(sheet.getByText('Classical ballet technique.')).toBeInTheDocument();
    // A regular class has no kind badge and is not canceled.
    expect(sheet.queryByText('Class')).not.toBeInTheDocument();
    expect(sheet.queryByText('Canceled')).not.toBeInTheDocument();
    expect(sheet.queryByText('Members only')).not.toBeInTheDocument();
  });

  it('marks a lesson for members', async () => {
    mockLessons();

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonContemporary });

    const dialog = await screen.findByRole('dialog', { name: 'Contemporary' });
    expect(within(dialog).getByText('Members only')).toBeInTheDocument();
  });

  it('marks a hidden lesson', async () => {
    mockLessons();

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonWorkshop });

    const dialog = await screen.findByRole('dialog', { name: 'Jazz' });
    expect(within(dialog).getByText('Hidden – staff only')).toBeInTheDocument();
  });

  it('marks a canceled lesson', async () => {
    mockLessons();

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonContemporary });

    const dialog = await screen.findByRole('dialog', { name: 'Contemporary' });
    expect(within(dialog).getByText('Canceled')).toBeInTheDocument();
    expect(within(dialog).getByText('This lesson has been canceled.')).toBeInTheDocument();
    expect(within(dialog).queryByText('Accompanist')).not.toBeInTheDocument();
  });

  it('shows the kind of a one-off lesson and no level', async () => {
    mockLessons();

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonWorkshop });

    const dialog = await screen.findByRole('dialog', { name: 'Jazz' });
    expect(within(dialog).getByText('Workshop')).toBeInTheDocument();
    expect(within(dialog).getByText('Saturday, November 7')).toBeInTheDocument();
    expect(within(dialog).getByText('14:00–17:00')).toBeInTheDocument();
    expect(within(dialog).queryByText('All levels')).not.toBeInTheDocument();
  });

  it('titles a room rental without a course type "Room rental", without a kind badge', async () => {
    mockLessons();

    renderAtSchedule(<LessonSheet />, { lesson: rentalId });

    const dialog = await screen.findByRole('dialog', { name: 'Room rental' });
    expect(within(dialog).getAllByText('Room rental')).toHaveLength(1);
    expect(within(dialog).getByText('Copperleaf Theater Group')).toBeInTheDocument();
  });

  it('stays closed without a lesson param', async () => {
    const get = mockLessons();

    renderAtSchedule(
      <>
        <p>page</p>
        <LessonSheet />
      </>,
    );

    expect(await screen.findByText('page')).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', expect.anything());
  });

  it('tells the visitor when the lesson is not available', async () => {
    mockLessons();
    const unknown = '0190a000-0000-7000-8000-000000000999';

    renderAtSchedule(<LessonSheet />, { lesson: unknown });

    const dialog = await screen.findByRole('dialog', { name: 'This lesson is not available' });
    expect(
      within(dialog).getByText('It may have been removed or is not released yet.'),
    ).toBeInTheDocument();
    expect(within(dialog).queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument();
  });

  it('offers a retry when loading fails for another reason', async () => {
    const get = mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/lessons/{lessonId}': { status: 500, body: { title: 'Internal Server Error' } },
    });
    const user = userEvent.setup();

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });

    const dialog = await screen.findByRole('dialog', { name: 'Lesson details' });
    const alert = await within(dialog).findByRole('alert');
    expect(alert).toHaveTextContent('Could not load the lesson.');

    get.mockClear();
    await user.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(get).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', expect.anything());
  });

  it('removes the lesson param when closed with the close button', async () => {
    mockLessons();
    const user = userEvent.setup();

    const { router } = renderAtSchedule(<LessonSheet />, {
      lesson: ids.lessonBallet,
      view: 'week',
    });
    await screen.findByRole('dialog', { name: 'Ballet' });

    await user.click(screen.getByRole('button', { name: 'Close' }));

    expect(router.state.location.search).toEqual({ view: 'week' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('removes the lesson param when closed with Escape', async () => {
    mockLessons();
    const user = userEvent.setup();

    const { router } = renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });
    await screen.findByRole('dialog', { name: 'Ballet' });

    await user.keyboard('{Escape}');

    expect(router.state.location.search).toEqual({});
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('closes without a history entry, so Back does not reopen it', async () => {
    mockLessons();
    const user = userEvent.setup();
    const { router } = renderAtSchedule(<LessonSheet />);
    await router.navigate({ to: '/schedule', search: { lesson: ids.lessonBallet } });

    await user.click(await screen.findByRole('button', { name: 'Close' }));
    expect(router.state.location.search).toEqual({});

    router.history.back();
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(router.state.location.search).toEqual({});
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockLessons();
    await i18n.changeLanguage('de');

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonContemporary });

    const dialog = await screen.findByRole('dialog', { name: 'Contemporary' });
    expect(within(dialog).getByText('Dienstag, 27. Oktober')).toBeInTheDocument();
    expect(within(dialog).getByText('Diese Stunde fällt aus.')).toBeInTheDocument();
    expect(within(dialog).getByText('10 Plätze')).toBeInTheDocument();
    expect(within(dialog).getByRole('button', { name: 'Schließen' })).toBeInTheDocument();
  });
});
