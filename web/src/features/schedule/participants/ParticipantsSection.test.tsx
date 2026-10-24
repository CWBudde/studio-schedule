import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { LessonDetail, Me } from '@/api/client';
import type {
  BookingEvent,
  LessonParticipant,
  LessonParticipants,
  StaffBooking,
} from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import {
  conflict,
  family,
  ids,
  lessonBalletDetail,
  lessonBalletOffice,
  meMember,
  meOffice,
  type MockRequest,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { LessonSheet } from '../detail/LessonSheet';

// The ballet lesson runs Monday 2026-10-26 18:00–19:30 in Berlin; the
// instructor's day ends at midnight in Berlin.
const beforeStart = '2026-10-23T10:00:00Z';
const duringLesson = '2026-10-26T17:30:00Z';
const afterDay = '2026-10-26T23:30:00Z';
const dayOverAt = '2026-10-26T23:00:00Z';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(beforeStart));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

/** The fictional instructor of the ballet lesson, as GET /auth/me returns her. */
const meInstructor: Me = {
  accountId: id(361),
  personId: ids.personAlex,
  email: 'alex.example@example.com',
  role: 'instructor',
  givenName: 'Alex',
  familyName: 'Example',
};

const booking = (
  n: number,
  name: string,
  extra: Partial<LessonParticipant> = {},
): LessonParticipant => ({
  bookingId: id(700 + n),
  name,
  guest: false,
  status: 'booked',
  source: 'self',
  trial: false,
  late: false,
  bookedAt: '2026-10-20T10:00:00Z',
  ...extra,
});

const mia = booking(1, 'Mia Sample', {
  personId: ids.personMia,
  contactEmail: 'mia.sample@example.org',
  contactPhone: '+49 000 0000201',
});
const gina = booking(2, 'Gina Guest', {
  guest: true,
  source: 'staff',
  contactEmail: 'gina.guest@example.com',
});
const noah = booking(3, 'Noah Sample', {
  personId: ids.personNoah,
  source: 'staff',
  trial: true,
  trialNote: 'First ballet class; a little shy.',
  contactEmail: 'mia.sample@example.org',
});
const carla = booking(4, 'Carla Example', {
  personId: id(344),
  status: 'canceled',
  late: true,
  canceledAt: '2026-10-25T20:00:00Z',
});

/** The participants of the ballet lesson: three booked, a late cancellation, one waiting. */
const participants: LessonParticipants = {
  lessonId: ids.lessonBallet,
  capacity: 12,
  active: 3,
  held: 0,
  freePlaces: 9,
  waiting: 1,
  dayOverAt,
  bookings: [mia, gina, noah, carla],
  waitlist: [
    {
      entryId: id(801),
      personId: id(345),
      name: 'Wendy Waiting',
      status: 'waiting',
      position: 1,
      stayOnList: false,
      joinedAt: '2026-10-21T10:00:00Z',
    },
  ],
};

/** The answer of a staff write. */
const staffBooking = (extra: Partial<StaffBooking> = {}): StaffBooking => ({
  id: id(790),
  lessonId: ids.lessonBallet,
  status: 'booked',
  source: 'staff',
  trial: false,
  late: false,
  bookedAt: beforeStart,
  ...extra,
});

type Answer = { status?: number; body: unknown };

/** A 422 at the path, with the reason the API gives (if any). */
const refusedAtPath = (location: string, reason?: string) => ({
  status: 422,
  body: {
    status: 422,
    title: 'Unprocessable Entity',
    detail: 'refused',
    errors: [{ location }],
    ...(reason ? { reason } : {}),
  },
});

/**
 * The API as `me` sees the ballet lesson; `list` answers the participants
 * (a 404 for a lesson the caller does not teach).
 */
function mockStaff({
  me = meInstructor,
  list = { body: participants },
  lesson = me.role === 'office' ? lessonBalletOffice : lessonBalletDetail,
}: {
  me?: Me;
  /** The participants' answer, or a function giving each answer in turn. */
  list?: Answer | (() => Answer);
  lesson?: LessonDetail;
} = {}) {
  return mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: me },
    '/api/v1/family': { body: { ...family, dependents: [] } },
    '/api/v1/lessons/{lessonId}': { body: lesson },
    '/api/v1/lessons/{lessonId}/participants': list,
    '/api/v1/staff/persons': (init: MockRequest) => {
      const q = String(init.params?.query?.q ?? '').toLowerCase();
      const all = [
        { id: id(346), name: 'Ute Tallowfield', birthYear: 1961 },
        { id: id(347), name: 'Uwe Tallowfield' },
      ];
      return { body: { items: all.filter((p) => p.name.toLowerCase().includes(q)) } };
    },
    '/api/v1/admin/bookings/{bookingId}/events': {
      body: {
        items: [
          {
            id: id(901),
            at: '2026-08-31T07:00:00Z',
            status: 'booked',
            late: false,
            override: false,
          },
          {
            id: id(902),
            at: '2026-10-26T17:40:00Z',
            status: 'attended',
            late: false,
            override: true,
            note: 'waitlist',
            actor: { accountId: id(361), name: 'Alex Example', email: 'alex.example@example.com' },
          },
        ] satisfies BookingEvent[],
      },
    },
  });
}

/** Opens the ballet lesson's sheet and returns its participants section. */
async function openParticipants() {
  render(<Toaster />);
  renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });
  const dialog = await screen.findByRole('dialog', { name: 'Ballet' });
  const section = await within(dialog).findByRole('region', { name: 'Participants' });
  return within(section);
}

describe('ParticipantsSection', () => {
  it('shows the office the participants with markers, contacts and the waiting list', async () => {
    mockStaff({ me: meOffice });

    const section = await openParticipants();

    expect(section.getByText('3 of 12 booked · 9 places free · 1 waiting')).toBeInTheDocument();
    const list = within(section.getByRole('list', { name: 'Booked participants' }));
    const rows = list.getAllByRole('listitem');
    expect(rows).toHaveLength(3);

    const miaRow = within(rows[0]!);
    expect(miaRow.getByText('Mia Sample')).toBeInTheDocument();
    expect(miaRow.getByText('Booked')).toBeInTheDocument();
    expect(miaRow.getByRole('link', { name: 'mia.sample@example.org' })).toHaveAttribute(
      'href',
      'mailto:mia.sample@example.org',
    );
    expect(miaRow.getByRole('link', { name: '+49 000 0000201' })).toHaveAttribute(
      'href',
      'tel:+490000000201',
    );

    const ginaRow = within(rows[1]!);
    expect(ginaRow.getByText('Guest')).toBeInTheDocument();
    expect(ginaRow.getByRole('link', { name: 'gina.guest@example.com' })).toBeInTheDocument();

    const noahRow = within(rows[2]!);
    expect(noahRow.getByText('Trial', { selector: '[data-slot=badge]' })).toBeInTheDocument();
    expect(noahRow.getByText('Note: First ballet class; a little shy.')).toBeInTheDocument();

    const canceled = within(section.getByRole('list', { name: 'Canceled bookings' }));
    expect(canceled.getByText('Carla Example')).toBeInTheDocument();
    expect(canceled.getByText('Canceled')).toBeInTheDocument();
    expect(canceled.getByText('Late')).toBeInTheDocument();
    expect(canceled.queryByRole('button', { name: 'Remove Carla Example' })).toBeNull();

    const waitlist = within(section.getByRole('list', { name: 'Waiting list' }));
    expect(waitlist.getByText('Wendy Waiting')).toBeInTheDocument();
    expect(waitlist.getByText('Position 1')).toBeInTheDocument();

    expect(section.getByRole('button', { name: 'Add participant' })).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Booking history of Mia Sample' })).toBeVisible();
    // Before the start, there is no attendance to take.
    expect(section.queryByRole('button', { name: 'Mia Sample attended' })).toBeNull();
    expect(section.queryByRole('button', { name: 'All attended' })).toBeNull();
  });

  it('stays away from a lesson the instructor does not teach', async () => {
    const get = mockStaff({ list: problem(404, 'not found') });

    render(<Toaster />);
    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });
    const dialog = await screen.findByRole('dialog', { name: 'Ballet' });
    await within(dialog).findByRole('region', { name: 'Booking' });
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/participants', {
        params: { path: { lessonId: ids.lessonBallet } },
        signal: expect.anything(),
      }),
    );
    expect(within(dialog).queryByRole('region', { name: 'Participants' })).toBeNull();
  });

  it('does not ask for the participants for a member', async () => {
    const get = mockStaff({ me: meMember });

    render(<Toaster />);
    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });
    const dialog = await screen.findByRole('dialog', { name: 'Ballet' });
    await within(dialog).findByRole('region', { name: 'Booking' });
    expect(within(dialog).queryByRole('region', { name: 'Participants' })).toBeNull();
    expect(get).not.toHaveBeenCalledWith(
      '/api/v1/lessons/{lessonId}/participants',
      expect.anything(),
    );
  });

  it('adds a person found by the search as a trial with a note', async () => {
    const get = mockStaff();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/participants': {
        status: 201,
        body: staffBooking({ personId: id(346), trial: true }),
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    // An instructor sees no booking history.
    expect(section.queryByRole('button', { name: /^Booking history of/ })).toBeNull();
    await user.click(section.getByRole('button', { name: 'Add participant' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Add participant' }));

    await user.click(dialog.getByRole('combobox', { name: 'Person' }));
    const search = await screen.findByRole('combobox', { name: 'Search by name' });
    await user.type(search, 'u');
    expect(await screen.findByText('Type at least 2 letters.')).toBeInTheDocument();
    await user.type(search, 'te');
    const option = await screen.findByRole('option', { name: /Ute Tallowfield/ });
    expect(option).toHaveTextContent('born 1961');
    expect(get).toHaveBeenCalledWith('/api/v1/staff/persons', {
      params: { query: { q: 'ute' } },
      signal: expect.anything(),
    });
    await user.click(option);
    expect(dialog.getByRole('combobox', { name: 'Person' })).toHaveTextContent('Ute Tallowfield');

    await user.click(dialog.getByRole('switch', { name: 'Trial lesson' }));
    await user.type(
      dialog.getByRole('textbox', { name: 'Note for the instructor (optional)' }),
      'Mind her left knee.',
    );
    await user.click(dialog.getByRole('button', { name: 'Add' }));

    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/participants', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { personId: id(346), trial: true, trialNote: 'Mind her left knee.' },
    });
    expect(await screen.findByText('Ute Tallowfield has been added.')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Add participant' })).toBeNull(),
    );
  });

  it('asks for a person before adding', async () => {
    mockStaff();
    const post = mockApiPost({});
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Add participant' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Add participant' }));
    await user.click(dialog.getByRole('button', { name: 'Add' }));

    expect(await dialog.findByText('Please choose a person.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('adds a guest without an email', async () => {
    mockStaff();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/participants': {
        status: 201,
        body: staffBooking({ guestName: 'Pia Walkby' }),
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Add participant' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Add participant' }));
    await user.click(dialog.getByRole('tab', { name: 'Guest' }));
    await user.type(dialog.getByRole('textbox', { name: 'Name' }), 'Pia Walkby');
    await user.click(dialog.getByRole('button', { name: 'Add' }));

    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/participants', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { guest: { name: 'Pia Walkby' } },
    });
    expect(await screen.findAllByText('Pia Walkby has been added.')).not.toHaveLength(0);
  });

  it('marks a bad guest email', async () => {
    mockStaff();
    const post = mockApiPost({});
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Add participant' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Add participant' }));
    await user.click(dialog.getByRole('tab', { name: 'Guest' }));
    await user.type(dialog.getByRole('textbox', { name: 'Name' }), 'Pia Walkby');
    await user.type(dialog.getByRole('textbox', { name: 'Email (optional)' }), 'pia@');
    await user.click(dialog.getByRole('button', { name: 'Add' }));

    expect(await dialog.findByText('Please enter a valid email address.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('books beyond capacity once confirmed, as the form is now', async () => {
    mockStaff();
    const bodies: unknown[] = [];
    mockApiPost({
      '/api/v1/lessons/{lessonId}/participants': (init: MockRequest) => {
        bodies.push(init.body);
        return (init.body as { confirmOverCapacity?: boolean }).confirmOverCapacity
          ? { status: 201, body: staffBooking({ guestName: 'Pia Walkby' }) }
          : conflict('full');
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Add participant' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Add participant' }));
    await user.click(dialog.getByRole('tab', { name: 'Guest' }));
    const name = dialog.getByRole('textbox', { name: 'Name' });
    await user.type(name, 'Pia Walkbi');
    await user.type(dialog.getByRole('textbox', { name: 'Email (optional)' }), 'pia@example.com');
    await user.click(dialog.getByRole('button', { name: 'Add' }));
    expect(await dialog.findByText('This lesson is full.')).toBeInTheDocument();

    // A change to the form clears the answer: it was about other values.
    await user.clear(name);
    await user.type(name, 'Pia Walkby');
    expect(dialog.queryByText('This lesson is full.')).toBeNull();
    await user.click(dialog.getByRole('button', { name: 'Add' }));
    await user.click(await dialog.findByRole('button', { name: 'Book anyway beyond capacity' }));

    await waitFor(() => expect(bodies).toHaveLength(3));
    expect(bodies).toEqual([
      { guest: { name: 'Pia Walkbi', email: 'pia@example.com' } },
      { guest: { name: 'Pia Walkby', email: 'pia@example.com' } },
      { guest: { name: 'Pia Walkby', email: 'pia@example.com' }, confirmOverCapacity: true },
    ]);
    expect(await screen.findAllByText('Pia Walkby has been added.')).not.toHaveLength(0);
  });

  it('says why an add was refused', async () => {
    mockStaff();
    mockApiPost({
      '/api/v1/lessons/{lessonId}/participants': conflict('already-booked'),
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Add participant' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Add participant' }));
    await user.click(dialog.getByRole('combobox', { name: 'Person' }));
    await user.type(await screen.findByRole('combobox', { name: 'Search by name' }), 'ute');
    await user.click(await screen.findByRole('option', { name: /Ute Tallowfield/ }));
    await user.click(dialog.getByRole('button', { name: 'Add' }));

    expect(
      await dialog.findByText('Ute Tallowfield is booked for this lesson already.'),
    ).toBeInTheDocument();
  });

  it('removes a participant after a confirmation', async () => {
    mockStaff();
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/remove': {
        body: staffBooking({ id: gina.bookingId, status: 'canceled' }),
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Remove Gina Guest' }));
    const confirm = within(await screen.findByRole('alertdialog', { name: 'Remove Gina Guest?' }));
    expect(
      confirm.getByText(/They get an email about it if there is an address\.$/),
    ).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
    await user.click(confirm.getByRole('button', { name: 'Remove' }));

    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/remove', {
      params: { path: { bookingId: gina.bookingId } },
    });
    expect(await screen.findByText('Gina Guest has been removed.')).toBeInTheDocument();
  });

  it('edits the note of a trial', async () => {
    mockStaff();
    const put = mockApiPut({
      '/api/v1/bookings/{bookingId}/participant': {
        body: staffBooking({ id: noah.bookingId, trial: true }),
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Trial lesson of Noah Sample' }));
    const dialog = within(
      await screen.findByRole('dialog', { name: 'Trial lesson of Noah Sample' }),
    );
    expect(dialog.getByRole('switch', { name: 'Trial lesson' })).toBeChecked();
    const note = dialog.getByRole('textbox', { name: 'Note for the instructor (optional)' });
    expect(note).toHaveValue('First ballet class; a little shy.');
    await user.clear(note);
    await user.type(note, 'Knows the basics.');
    await user.click(dialog.getByRole('button', { name: 'Save' }));

    expect(put).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/participant', {
      params: { path: { bookingId: noah.bookingId } },
      body: { trial: true, trialNote: 'Knows the basics.' },
    });
    expect(
      await screen.findByText('The trial lesson of Noah Sample has been saved.'),
    ).toBeInTheDocument();
  });

  it('takes attendance from the start of the lesson', async () => {
    vi.setSystemTime(new Date(duringLesson));
    mockStaff();
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/attendance': {
        body: staffBooking({ id: mia.bookingId, status: 'attended' }),
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    const attended = section.getByRole('button', { name: 'Mia Sample attended' });
    expect(attended).toHaveAttribute('aria-pressed', 'false');
    expect(section.getByRole('button', { name: 'Mia Sample did not come' })).toBeInTheDocument();
    // Nothing to reset while the attendance is open.
    expect(
      section.queryByRole('button', { name: 'Reset the attendance of Mia Sample' }),
    ).toBeNull();
    await user.click(attended);

    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/attendance', {
      params: { path: { bookingId: mia.bookingId } },
      body: { status: 'attended' },
    });
    expect(await screen.findByText('Mia Sample attended.')).toBeInTheDocument();
  });

  it('resets a recorded attendance', async () => {
    vi.setSystemTime(new Date(duringLesson));
    mockStaff({
      list: {
        body: { ...participants, bookings: [{ ...mia, status: 'no_show' }] },
      },
    });
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/attendance': { body: staffBooking({ id: mia.bookingId }) },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    expect(section.getByText('No-show', { selector: '[data-slot=badge]' })).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Mia Sample did not come' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    await user.click(section.getByRole('button', { name: 'Reset the attendance of Mia Sample' }));

    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/attendance', {
      params: { path: { bookingId: mia.bookingId } },
      body: { status: 'booked' },
    });
  });

  it('marks everyone attended at once', async () => {
    vi.setSystemTime(new Date(duringLesson));
    mockStaff();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/attendance/all': {
        body: {
          items: [mia, gina, noah].map((b) =>
            staffBooking({ id: b.bookingId, status: 'attended' }),
          ),
        },
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'All attended' }));

    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/attendance/all', {
      params: { path: { lessonId: ids.lessonBallet } },
    });
    expect(await screen.findByText('3 participants marked as attended.')).toBeInTheDocument();
  });

  it('leaves the changes to the office once the instructor’s day is over', async () => {
    vi.setSystemTime(new Date(afterDay));
    mockStaff();

    const section = await openParticipants();

    expect(
      section.getByText('The day of this lesson is over: the office makes changes now.'),
    ).toBeInTheDocument();
    expect(section.queryByRole('button', { name: 'Add participant' })).toBeNull();
    expect(section.queryByRole('button', { name: 'Mia Sample attended' })).toBeNull();
    expect(section.queryByRole('button', { name: 'Remove Mia Sample' })).toBeNull();
    expect(section.queryByRole('button', { name: 'All attended' })).toBeNull();
  });

  it('lets the office take attendance after the day', async () => {
    vi.setSystemTime(new Date(afterDay));
    mockStaff({ me: meOffice });

    const section = await openParticipants();

    expect(section.getByRole('button', { name: 'Mia Sample attended' })).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Add participant' })).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'All attended' })).toBeInTheDocument();
  });

  it('says so when the API finds the instructor’s day over', async () => {
    vi.setSystemTime(new Date(duringLesson));
    mockStaff();
    mockApiPost({
      '/api/v1/bookings/{bookingId}/attendance': refusedAtPath('path.bookingId', 'day-over'),
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Gina Guest did not come' }));

    expect(
      await screen.findByText('The day of this lesson is over: the office can make the change.'),
    ).toBeInTheDocument();
  });

  it('says so when the API finds the lesson not started', async () => {
    vi.setSystemTime(new Date(duringLesson));
    mockStaff();
    mockApiPost({
      '/api/v1/bookings/{bookingId}/attendance': refusedAtPath('path.bookingId', 'not-started'),
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Mia Sample did not come' }));

    expect(
      await screen.findByText('Attendance can be taken from the start of the lesson.'),
    ).toBeInTheDocument();
  });

  it('gives a neutral reason for a refusal at the path without one', async () => {
    vi.setSystemTime(new Date(duringLesson));
    mockStaff();
    mockApiPost({
      '/api/v1/bookings/{bookingId}/attendance': refusedAtPath('path.bookingId'),
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Noah Sample did not come' }));

    expect(
      await screen.findByText(/^This is not possible now: attendance opens at the start/),
    ).toBeInTheDocument();
  });

  it('keeps the list and an open dialog when a refetch fails', async () => {
    let calls = 0;
    mockStaff({
      list: () => (++calls === 1 ? { body: participants } : problem(500, 'boom')),
    });
    mockApiPost({
      '/api/v1/bookings/{bookingId}/attendance': { body: staffBooking() },
      '/api/v1/bookings/{bookingId}/remove': conflict('already-canceled'),
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Remove Gina Guest' }));
    const confirm = within(await screen.findByRole('alertdialog', { name: 'Remove Gina Guest?' }));
    await user.click(confirm.getByRole('button', { name: 'Remove' }));

    // The refusal refreshes the participants, which fails: the list stays,
    // with a warning, and so does the dialog with its answer.
    expect(
      await section.findByText(
        'The participants could not be updated; this may not be the latest state.',
      ),
    ).toBeInTheDocument();
    expect(section.getByText('Mia Sample')).toBeInTheDocument();
    expect(confirm.getByText('This booking was canceled already.')).toBeInTheDocument();
    expect(screen.getByRole('alertdialog', { name: 'Remove Gina Guest?' })).toBeInTheDocument();
  });

  it('promises no mail when removing from a canceled lesson', async () => {
    mockStaff({ lesson: { ...lessonBalletDetail, status: 'canceled' } });
    const user = userEvent.setup();

    const section = await openParticipants();
    // Nobody is added to a canceled lesson, but participants may go.
    expect(section.queryByRole('button', { name: 'Add participant' })).toBeNull();
    await user.click(section.getByRole('button', { name: 'Remove Gina Guest' }));
    const confirm = within(await screen.findByRole('alertdialog', { name: 'Remove Gina Guest?' }));
    expect(
      confirm.getByText(/The lesson is canceled, so nobody gets an email\.$/),
    ).toBeInTheDocument();
  });

  it('names a person the API no longer finds at the picker', async () => {
    mockStaff();
    mockApiPost({
      '/api/v1/lessons/{lessonId}/participants': problem(422, 'unknown person', [
        { location: 'body.personId' },
      ]),
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Add participant' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Add participant' }));
    await user.click(dialog.getByRole('combobox', { name: 'Person' }));
    await user.type(await screen.findByRole('combobox', { name: 'Search by name' }), 'ute');
    await user.click(await screen.findByRole('option', { name: /Ute Tallowfield/ }));
    await user.click(dialog.getByRole('button', { name: 'Add' }));

    expect(await dialog.findByText('This person cannot be found any more.')).toBeInTheDocument();
    expect(dialog.getByRole('combobox', { name: 'Person' })).toHaveAttribute(
      'aria-invalid',
      'true',
    );
  });

  it('counts a trial note in characters, not code units', async () => {
    mockStaff();
    const put = mockApiPut({
      '/api/v1/bookings/{bookingId}/participant': {
        body: staffBooking({ id: noah.bookingId, trial: true }),
      },
    });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Trial lesson of Noah Sample' }));
    const dialog = within(
      await screen.findByRole('dialog', { name: 'Trial lesson of Noah Sample' }),
    );
    const note = dialog.getByRole('textbox', { name: 'Note for the instructor (optional)' });
    // 500 characters, but 1000 UTF-16 code units.
    await user.clear(note);
    await user.click(note);
    await user.paste('💃'.repeat(500));
    await user.click(dialog.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(put).toHaveBeenCalledTimes(1));

    await user.click(section.getByRole('button', { name: 'Trial lesson of Noah Sample' }));
    const again = within(
      await screen.findByRole('dialog', { name: 'Trial lesson of Noah Sample' }),
    );
    const second = again.getByRole('textbox', { name: 'Note for the instructor (optional)' });
    await user.clear(second);
    await user.click(second);
    await user.paste('💃'.repeat(501));
    await user.click(again.getByRole('button', { name: 'Save' }));
    expect(await again.findByText('This entry is too long.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledTimes(1);
  });

  it('shows the office the history of a booking', async () => {
    mockStaff({ me: meOffice });
    const user = userEvent.setup();

    const section = await openParticipants();
    await user.click(section.getByRole('button', { name: 'Booking history of Mia Sample' }));
    const dialog = within(await screen.findByRole('dialog', { name: 'Booking history' }));

    const events = within(await dialog.findByRole('list', { name: 'Booking history' }));
    const items = events.getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(within(items[0]!).getByText('Booked')).toBeInTheDocument();
    expect(within(items[0]!).getByText('automatically')).toBeInTheDocument();
    expect(within(items[1]!).getByText('Attended')).toBeInTheDocument();
    expect(within(items[1]!).getByText('Beyond capacity')).toBeInTheDocument();
    expect(within(items[1]!).getByText('From the waiting list')).toBeInTheDocument();
    expect(
      within(items[1]!).getByText('by Alex Example (alex.example@example.com)'),
    ).toBeInTheDocument();
  });
});
