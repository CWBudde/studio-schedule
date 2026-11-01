import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent, { type UserEvent } from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  catalogFixtures,
  conflict,
  ids,
  lessonBalletOffice,
  lessonWorkshopOffice,
  meOffice,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
  publicSettings,
  staffEntries,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { LessonFormSheet, type LessonFormTarget } from './LessonFormSheet';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-23T10:00:00Z'));
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: meOffice },
    '/api/v1/admin/staff': { body: { items: staffEntries } },
  });
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function renderSheet(target: LessonFormTarget, onCreated = vi.fn()) {
  const onClose = vi.fn();
  render(<Toaster />);
  renderAtSchedule(<LessonFormSheet target={target} onClose={onClose} onCreated={onCreated} />);
  return { onClose, onCreated };
}

/** Picks `option` in the select `label` of `scope`. */
async function pick(
  user: UserEvent,
  scope: ReturnType<typeof within>,
  label: string,
  option: string,
) {
  await user.click(scope.getByRole('combobox', { name: label }));
  await user.click(await screen.findByRole('option', { name: option }));
}

/** Replaces the text of the input `label` of `scope`. */
async function fill(
  user: UserEvent,
  scope: ReturnType<typeof within>,
  label: string,
  text: string,
) {
  const input = scope.getByLabelText(label);
  await user.clear(input);
  await user.type(input, text);
}

describe('LessonFormSheet', () => {
  it('shows the lesson in the school time zone and saves the edit', async () => {
    const put = mockApiPut({ '/api/v1/lessons/{lessonId}': { body: lessonBalletOffice } });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'edit', lesson: lessonBalletOffice });

    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    expect(await sheet.findByLabelText('Date')).toHaveValue('2026-10-26');
    expect(sheet.getByText(/^Ballet/)).toBeInTheDocument();
    expect(sheet.getByLabelText('Start time')).toHaveValue('18:00');
    expect(sheet.getByLabelText('Duration (minutes)')).toHaveValue(90);
    expect(sheet.getByRole('combobox', { name: 'Room' })).toHaveTextContent(
      'North Studio · Room A',
    );
    expect(sheet.getByRole('combobox', { name: 'Instructor' })).toHaveTextContent('Alex Example');
    expect(sheet.getByRole('combobox', { name: 'Accompanist' })).toHaveTextContent('Sam Pianist');
    expect(sheet.getByLabelText('Capacity')).toHaveValue(12);
    expect(sheet.getByRole('combobox', { name: 'Visibility' })).toHaveTextContent('Public');
    expect(sheet.getByRole('combobox', { name: 'Kind' })).toHaveTextContent('Regular lesson');
    // The course type of a lesson stays; there is no select for it.
    expect(sheet.queryByRole('combobox', { name: 'Course type' })).toBeNull();
    expect(sheet.getByText(/Afterwards it no longer follows its class\./)).toBeInTheDocument();
    expect(
      sheet.getByText(/Everyone booked gets a mail about a new start or room\./),
    ).toBeInTheDocument();

    await fill(user, sheet, 'Start time', '19:00');
    await pick(user, sheet, 'Room', 'South Studio · Room B');
    await pick(user, sheet, 'Accompanist', 'None');
    await pick(user, sheet, 'Kind', 'Workshop');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(put).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: {
        date: '2026-10-26',
        startTime: '19:00',
        durationMinutes: 90,
        roomId: ids.roomB,
        instructorId: ids.staffAlex,
        // A room change keeps the capacity of an existing lesson.
        capacity: 12,
        visibility: 'public',
        kind: 'workshop',
      },
    });
    expect(await screen.findByText('The lesson has been saved.')).toBeInTheDocument();
  });

  it('names the lesson that has the room and saves anyway on request', async () => {
    const put = mockApiPut({
      '/api/v1/lessons/{lessonId}': ({ body }) =>
        (body as { confirmRoomOverlap?: boolean }).confirmRoomOverlap
          ? { body: lessonBalletOffice }
          : conflict('room-taken', 'room taken', {
              lesson: {
                id: ids.lessonContemporary,
                courseType: { id: ids.contemporary, name: 'Contemporary' },
                startsAt: '2026-10-26T16:30:00Z',
                endsAt: '2026-10-26T17:30:00Z',
              },
            }),
    });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'edit', lesson: lessonBalletOffice });
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    await sheet.findByLabelText('Date');

    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    const alert = await sheet.findByRole('alert');
    expect(alert).toHaveTextContent('Room A is taken at that time');
    expect(alert).toHaveTextContent('Contemporary, Monday, October 26, 17:30–18:30 uses the room.');
    expect(onClose).not.toHaveBeenCalled();

    await user.click(within(alert).getByRole('button', { name: 'Save anyway' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(put).toHaveBeenCalledTimes(2);
    const [first, second] = put.mock.calls.map((call) => (call[1] as { body: object }).body);
    expect(second).toEqual({ ...first, confirmRoomOverlap: true });
    expect(first).not.toHaveProperty('confirmRoomOverlap');
  });

  it('marks what the API refuses: a date, a capacity below the bookings', async () => {
    mockApiPut({
      '/api/v1/lessons/{lessonId}': problem(422, 'validation failed', [
        { location: 'body.date', message: 'invalid' },
        { location: 'body.capacity', message: 'below bookings' },
      ]),
    });
    const user = userEvent.setup();
    renderSheet({ mode: 'edit', lesson: lessonBalletOffice });
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    await sheet.findByLabelText('Date');

    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(
      await sheet.findByText('More people have booked this lesson than that.'),
    ).toBeInTheDocument();
    expect(sheet.getByText('Please check this entry.')).toBeInTheDocument();
    expect(sheet.getByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(sheet.getByLabelText('Date')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Capacity')).toHaveAttribute('aria-invalid', 'true');
  });

  it('says so when the lesson has started meanwhile (422 without a field)', async () => {
    mockApiPut({
      '/api/v1/lessons/{lessonId}': problem(422, 'the lesson has started', [
        { location: 'path.lessonId', message: 'the lesson has started' },
      ]),
    });
    const user = userEvent.setup();
    renderSheet({ mode: 'edit', lesson: lessonBalletOffice });
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    await sheet.findByLabelText('Date');

    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'This lesson can no longer be changed; it may have started meanwhile.',
    );
  });

  it('shows another 422 without a field as a general failure', async () => {
    mockApiPut({ '/api/v1/lessons/{lessonId}': problem(422, 'something else') });
    const user = userEvent.setup();
    renderSheet({ mode: 'edit', lesson: lessonBalletOffice });
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    await sheet.findByLabelText('Date');

    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    const alert = await sheet.findByRole('alert');
    expect(alert).toHaveTextContent('Something went wrong. Please try again.');
    expect(alert).not.toHaveTextContent('started');
  });

  it('saves anyway with what the form holds now, checked again', async () => {
    const put = mockApiPut({
      '/api/v1/lessons/{lessonId}': ({ body }) =>
        (body as { confirmRoomOverlap?: boolean }).confirmRoomOverlap
          ? { body: lessonBalletOffice }
          : conflict('room-taken', 'room taken', {
              lesson: {
                id: ids.lessonContemporary,
                courseType: { id: ids.contemporary, name: 'Contemporary' },
                startsAt: '2026-10-26T16:30:00Z',
                endsAt: '2026-10-26T17:30:00Z',
              },
            }),
    });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'edit', lesson: lessonBalletOffice });
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    await sheet.findByLabelText('Date');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));
    const alert = await sheet.findByRole('alert');

    // A capacity typed after the refusal goes along, but only once it is valid.
    await fill(user, sheet, 'Capacity', '0');
    await user.click(within(alert).getByRole('button', { name: 'Save anyway' }));
    expect(await sheet.findByText('Enter a whole number from 1 to 1000.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledTimes(1);

    await fill(user, sheet, 'Capacity', '8');
    await user.click(within(alert).getByRole('button', { name: 'Save anyway' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(put).toHaveBeenCalledTimes(2);
    expect(put.mock.calls[1]?.[1]).toMatchObject({
      body: {
        capacity: 8,
        confirmRoomOverlap: true,
      },
    });
  });

  it('confirms a lesson in the past, then a taken room, keeping both confirmations', async () => {
    const post = mockApiPost({
      '/api/v1/lessons': ({ body }) => {
        const { confirmPast, confirmRoomOverlap } = body as {
          confirmPast?: boolean;
          confirmRoomOverlap?: boolean;
        };
        if (!confirmPast) return conflict('past');
        if (!confirmRoomOverlap) {
          return conflict('room-taken', 'room taken', {
            lesson: {
              id: ids.lessonBallet,
              courseType: { id: ids.ballet, name: 'Ballet' },
              startsAt: '2026-10-19T17:00:00Z',
              endsAt: '2026-10-19T18:30:00Z',
            },
          });
        }
        return { status: 201, body: lessonWorkshopOffice };
      },
    });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'add' });
    const sheet = within(await screen.findByRole('dialog', { name: 'New one-off lesson' }));
    await sheet.findByRole('combobox', { name: 'Course type' });
    await pick(user, sheet, 'Course type', 'Jazz');
    await fill(user, sheet, 'Date', '2026-10-19');
    await fill(user, sheet, 'Start time', '18:00');
    await fill(user, sheet, 'Duration (minutes)', '60');
    await pick(user, sheet, 'Room', 'North Studio · Room A');
    await pick(user, sheet, 'Instructor', 'Riley Sample');
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));

    const past = await sheet.findByRole('alert');
    expect(past).toHaveTextContent('This lesson is in the past');
    expect(past).toHaveTextContent('nobody gets a mail about a lesson in the past.');
    await user.click(within(past).getByRole('button', { name: 'Save anyway' }));

    const taken = await sheet.findByText('Room A is taken at that time');
    const alert = taken.closest('[role="alert"]') as HTMLElement;
    expect(alert).toHaveTextContent('Ballet, Monday, October 19, 19:00–20:30 uses the room.');
    await user.click(within(alert).getByRole('button', { name: 'Save anyway' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    const bodies = post.mock.calls.map((call) => (call[1] as { body: object }).body);
    expect(bodies).toHaveLength(3);
    expect(bodies[0]).not.toHaveProperty('confirmPast');
    expect(bodies[1]).toEqual({ ...bodies[0], confirmPast: true });
    expect(bodies[2]).toEqual({ ...bodies[0], confirmPast: true, confirmRoomOverlap: true });
  });

  it('adds a one-off lesson with the capacity of its room', async () => {
    const post = mockApiPost({ '/api/v1/lessons': { status: 201, body: lessonWorkshopOffice } });
    const user = userEvent.setup();
    const { onClose, onCreated } = renderSheet({ mode: 'add' });

    const sheet = within(await screen.findByRole('dialog', { name: 'New one-off lesson' }));
    await sheet.findByRole('combobox', { name: 'Course type' });
    await pick(user, sheet, 'Course type', 'Jazz');
    await fill(user, sheet, 'Date', '2026-11-07');
    await fill(user, sheet, 'Start time', '14:00');
    await fill(user, sheet, 'Duration (minutes)', '180');
    await pick(user, sheet, 'Room', 'North Studio · Room A');
    expect(sheet.getByLabelText('Capacity')).toHaveValue(16);
    await pick(user, sheet, 'Instructor', 'Riley Sample');
    expect(sheet.getByRole('combobox', { name: 'Visibility' })).toHaveTextContent('Public');
    await pick(user, sheet, 'Kind', 'Workshop');
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(post).toHaveBeenCalledWith('/api/v1/lessons', {
      body: {
        courseTypeId: ids.jazz,
        date: '2026-11-07',
        startTime: '14:00',
        durationMinutes: 180,
        roomId: ids.roomA,
        instructorId: ids.staffRiley,
        capacity: 16,
        visibility: 'public',
        kind: 'workshop',
      },
    });
    expect(onCreated).toHaveBeenCalledWith(ids.lessonWorkshop);
    // Sonner keeps the toasts of earlier tests in this file.
    expect(await screen.findAllByText('The lesson has been added.')).not.toHaveLength(0);
  });

  it('adds a room rental: a renter instead of staff, places and visibility', async () => {
    const post = mockApiPost({ '/api/v1/lessons': { status: 201, body: lessonWorkshopOffice } });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'add' });

    const sheet = within(await screen.findByRole('dialog', { name: 'New one-off lesson' }));
    await sheet.findByRole('combobox', { name: 'Course type' });
    await pick(user, sheet, 'Course type', 'Jazz');
    await pick(user, sheet, 'Kind', 'Room rental');
    for (const name of ['Instructor', 'Accompanist', 'Visibility']) {
      expect(sheet.queryByRole('combobox', { name })).toBeNull();
    }
    expect(sheet.queryByLabelText('Capacity')).toBeNull();
    expect(sheet.getByText(/only staff see it, it has no instructor/)).toBeInTheDocument();

    await fill(user, sheet, 'Date', '2026-11-07');
    await fill(user, sheet, 'Start time', '14:00');
    await fill(user, sheet, 'Duration (minutes)', '120');
    await pick(user, sheet, 'Room', 'North Studio · Room A');
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));
    // The renter is needed.
    expect(await sheet.findByText('Please fill in this field.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();

    await fill(user, sheet, 'Rented by', '  Copperleaf Theater Group ');
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(post).toHaveBeenCalledWith('/api/v1/lessons', {
      body: {
        courseTypeId: ids.jazz,
        date: '2026-11-07',
        startTime: '14:00',
        durationMinutes: 120,
        roomId: ids.roomA,
        capacity: 0,
        visibility: 'hidden',
        kind: 'rental',
        renter: 'Copperleaf Theater Group',
      },
    });
  });

  it('edits a room rental and turns it into a lesson', async () => {
    const rental = {
      ...lessonWorkshopOffice,
      kind: 'rental' as const,
      visibility: 'hidden' as const,
      instructor: undefined,
      renter: 'Copperleaf Theater Group',
      capacity: 0,
      freePlaces: 0,
    };
    const put = mockApiPut({ '/api/v1/lessons/{lessonId}': { body: lessonWorkshopOffice } });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'edit', lesson: rental });

    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    expect(await sheet.findByLabelText('Rented by')).toHaveValue('Copperleaf Theater Group');
    expect(sheet.getByRole('combobox', { name: 'Kind' })).toHaveTextContent('Room rental');
    expect(sheet.queryByRole('combobox', { name: 'Instructor' })).toBeNull();

    // As a workshop it needs an instructor and a capacity again, and no renter.
    await pick(user, sheet, 'Kind', 'Workshop');
    expect(sheet.queryByLabelText('Rented by')).toBeNull();
    expect(sheet.getByLabelText('Capacity')).toHaveValue(null);
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));
    expect(await sheet.findByText('Please choose one.')).toBeInTheDocument();
    expect(sheet.getByText('Enter a whole number from 1 to 1000.')).toBeInTheDocument();
    expect(put).not.toHaveBeenCalled();

    await pick(user, sheet, 'Instructor', 'Riley Sample');
    await fill(user, sheet, 'Capacity', '12');
    await pick(user, sheet, 'Visibility', 'Public');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(put).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', {
      params: { path: { lessonId: ids.lessonWorkshop } },
      body: {
        date: '2026-11-07',
        startTime: '14:00',
        durationMinutes: 180,
        roomId: ids.roomA,
        instructorId: ids.staffRiley,
        capacity: 12,
        visibility: 'public',
        kind: 'workshop',
      },
    });
  });

  it('adds a room rental without a course type', async () => {
    const post = mockApiPost({ '/api/v1/lessons': { status: 201, body: lessonWorkshopOffice } });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'add' });

    const sheet = within(await screen.findByRole('dialog', { name: 'New one-off lesson' }));
    await sheet.findByRole('combobox', { name: 'Course type' });
    await pick(user, sheet, 'Course type', 'Jazz');
    await pick(user, sheet, 'Kind', 'Room rental');
    // For a room rental the course type is optional, and can be cleared.
    expect(sheet.getByText('Optional for a room rental.')).toBeInTheDocument();
    expect(sheet.getByRole('combobox', { name: 'Course type' })).toHaveTextContent('Jazz');
    await pick(user, sheet, 'Course type', 'None');
    expect(sheet.getByRole('combobox', { name: 'Course type' })).toHaveTextContent('None');

    await fill(user, sheet, 'Rented by', 'Copperleaf Theater Group');
    await fill(user, sheet, 'Date', '2026-11-07');
    await fill(user, sheet, 'Start time', '14:00');
    await fill(user, sheet, 'Duration (minutes)', '120');
    await pick(user, sheet, 'Room', 'North Studio · Room A');
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(post).toHaveBeenCalledWith('/api/v1/lessons', {
      body: {
        date: '2026-11-07',
        startTime: '14:00',
        durationMinutes: 120,
        roomId: ids.roomA,
        capacity: 0,
        visibility: 'hidden',
        kind: 'rental',
        renter: 'Copperleaf Theater Group',
      },
    });
  });

  it('requires a course type for any kind but a room rental', async () => {
    const post = mockApiPost({});
    const user = userEvent.setup();
    renderSheet({ mode: 'add' });

    const sheet = within(await screen.findByRole('dialog', { name: 'New one-off lesson' }));
    await sheet.findByRole('combobox', { name: 'Course type' });
    await pick(user, sheet, 'Kind', 'Room rental');
    await pick(user, sheet, 'Kind', 'Workshop');
    expect(sheet.queryByText('Optional for a room rental.')).toBeNull();
    await fill(user, sheet, 'Date', '2026-11-07');
    await fill(user, sheet, 'Start time', '14:00');
    await fill(user, sheet, 'Duration (minutes)', '120');
    await pick(user, sheet, 'Room', 'North Studio · Room A');
    await pick(user, sheet, 'Instructor', 'Riley Sample');
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));

    const courseType = sheet.getByRole('combobox', { name: 'Course type' });
    await waitFor(() => expect(courseType).toHaveAccessibleDescription('Please choose one.'));
    expect(sheet.getAllByText('Please choose one.')).toHaveLength(1);
    expect(post).not.toHaveBeenCalled();
  });

  it('keeps a room rental without a course type a room rental', async () => {
    const rental = {
      ...lessonWorkshopOffice,
      courseType: undefined,
      kind: 'rental' as const,
      visibility: 'hidden' as const,
      instructor: undefined,
      renter: 'Copperleaf Theater Group',
      capacity: 0,
      freePlaces: 0,
    };
    const put = mockApiPut({ '/api/v1/lessons/{lessonId}': { body: rental } });
    const user = userEvent.setup();
    const { onClose } = renderSheet({ mode: 'edit', lesson: rental });

    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    expect(await sheet.findByLabelText('Rented by')).toHaveValue('Copperleaf Theater Group');
    expect(sheet.getByText('Course type:').parentElement).toHaveTextContent('Course type: None');
    expect(sheet.queryByRole('combobox', { name: 'Course type' })).toBeNull();
    expect(
      sheet.getByText('A room rental without a course type stays a room rental.'),
    ).toBeInTheDocument();
    await user.click(sheet.getByRole('combobox', { name: 'Kind' }));
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Room rental']);
    await user.keyboard('{Escape}');

    await fill(user, sheet, 'Rented by', 'Bramble Choir');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(put).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', {
      params: { path: { lessonId: ids.lessonWorkshop } },
      body: {
        date: '2026-11-07',
        startTime: '14:00',
        durationMinutes: 180,
        roomId: ids.roomA,
        capacity: 0,
        visibility: 'hidden',
        kind: 'rental',
        renter: 'Bramble Choir',
      },
    });
  });

  it('offers no room rental for a lesson of a class', async () => {
    const user = userEvent.setup();
    renderSheet({ mode: 'edit', lesson: lessonBalletOffice });

    const sheet = within(await screen.findByRole('dialog', { name: 'Edit lesson' }));
    await user.click(await sheet.findByRole('combobox', { name: 'Kind' }));
    expect(await screen.findByRole('option', { name: 'Workshop' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Room rental' })).not.toBeInTheDocument();
  });

  it('checks the form before it asks the API', async () => {
    const post = mockApiPost({});
    const user = userEvent.setup();
    renderSheet({ mode: 'add' });
    const sheet = within(await screen.findByRole('dialog', { name: 'New one-off lesson' }));
    await sheet.findByRole('combobox', { name: 'Course type' });

    await fill(user, sheet, 'Start time', '23:30');
    await fill(user, sheet, 'Duration (minutes)', '60');
    await user.click(sheet.getByRole('button', { name: 'Add lesson' }));

    expect(await sheet.findByText('Enter a date.')).toBeInTheDocument();
    expect(sheet.getByText('The lesson must end by midnight.')).toBeInTheDocument();
    expect(sheet.getAllByText('Please choose one.')).toHaveLength(3);
    expect(post).not.toHaveBeenCalled();
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    renderSheet({ mode: 'add' });

    const sheet = within(await screen.findByRole('dialog', { name: 'Neuer Einzeltermin' }));
    expect(await sheet.findByRole('combobox', { name: 'Kursart' })).toBeInTheDocument();
    expect(sheet.getByRole('combobox', { name: 'Art' })).toHaveTextContent('Reguläre Stunde');
    expect(sheet.getByRole('button', { name: 'Stunde anlegen' })).toBeInTheDocument();
  });
});
