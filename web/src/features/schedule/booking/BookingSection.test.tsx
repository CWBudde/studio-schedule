import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, type LessonDetail } from '@/api/client';
import { type Booking, scheduleQuery, type WaitlistFor } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  family,
  ids,
  lessonBalletDetail,
  lessonContemporary,
  meMember,
  type MockRequest,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  schedule,
  signedOut,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { LessonSheet } from '../detail/LessonSheet';

// Friday 2026-10-23 12:00 in Berlin, before every fixture lesson.
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-23T10:00:00Z'));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const nora = { id: meMember.personId, name: 'Nora Example' };
const lina = { id: family.dependents[0]!.id, name: 'Lina Example' };
const bookingId = '0190a000-0000-7000-8000-000000000601';

const alone = { ...family, dependents: [] };

/** Nora's bookings of the ballet class from the booked lesson on. */
const followingBookings = [
  {
    bookingId,
    lessonId: ids.lessonBallet,
    startsAt: '2026-10-26T17:00:00Z',
    endsAt: '2026-10-26T18:30:00Z',
    late: true,
  },
  {
    bookingId: '0190a000-0000-7000-8000-000000000602',
    lessonId: '0190a000-0000-7000-8000-000000000112',
    startsAt: '2026-11-02T17:00:00Z',
    endsAt: '2026-11-02T18:30:00Z',
    late: false,
  },
  {
    bookingId: '0190a000-0000-7000-8000-000000000603',
    lessonId: '0190a000-0000-7000-8000-000000000113',
    startsAt: '2026-11-09T17:00:00Z',
    endsAt: '2026-11-09T18:30:00Z',
    late: false,
  },
];

/** The answer of bookLesson or cancelBooking. */
const bookingOf = (personId: string, extra: Partial<Booking> = {}): Booking => ({
  id: bookingId,
  lessonId: ids.lessonBallet,
  personId,
  status: 'booked',
  source: 'self',
  bookedAt: '2026-10-23T10:00:00Z',
  late: false,
  ...extra,
});

/**
 * The API as the member sees it (`me: null` for a visitor). `lesson()` is
 * read on every request, so a test can change what a refetch returns.
 */
function mockMember({
  lesson = () => lessonBalletDetail as LessonDetail,
  fam = alone,
  me = meMember as typeof meMember | null,
}: {
  lesson?: () => LessonDetail;
  fam?: typeof family;
  me?: typeof meMember | null;
} = {}) {
  return mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    ...(me ? { '/api/v1/auth/me': { body: me } } : signedOut),
    '/api/v1/family': me ? { body: fam } : problem(401, 'not signed in'),
    '/api/v1/lessons/{lessonId}': (init: MockRequest) => {
      const l = lesson();
      return init.params?.path?.lessonId === l.id
        ? { body: l }
        : { status: 404, body: { title: 'Not Found' } };
    },
    // Nora is a member of the class: the preview names her pause, up to
    // the date asked for (by default the end of the term).
    '/api/v1/bookings/{bookingId}/cancel-following': (init: MockRequest) => {
      const until = (init.params?.query as { until?: string } | undefined)?.until ?? '2027-07-31';
      return {
        body: {
          bookings: followingBookings.filter((b) => b.startsAt.slice(0, 10) <= until),
          until,
          pausedUntil: until,
        },
      };
    },
  });
}

/** The ballet lesson with Nora (and `more`) booked. */
const bookedBallet = (...more: { personId: string; name: string; bookingId: string }[]) => ({
  ...lessonBalletDetail,
  freePlaces: lessonBalletDetail.freePlaces - 1 - more.length,
  bookedFor: [{ personId: nora.id, name: nora.name, bookingId }, ...more],
});

/** Opens the sheet of `lessonId` and returns its booking section. */
async function openBooking(lessonId = ids.lessonBallet, name = 'Ballet', region = 'Booking') {
  render(<Toaster />);
  const rendered = renderAtSchedule(<LessonSheet />, { lesson: lessonId });
  const dialog = await screen.findByRole('dialog', { name });
  const section = await within(dialog).findByRole('region', { name: region });
  return { ...rendered, section: within(section) };
}

/** A promise with its resolve function, e.g. to hold an API answer back. */
function deferred<T>() {
  let resolve: (value: T) => void = () => undefined;
  const promise = new Promise<T>((r) => (resolve = r));
  return { promise, resolve };
}

describe('BookingSection', () => {
  it('books the lesson for the signed-in member', async () => {
    let current: LessonDetail = lessonBalletDetail;
    mockMember({ lesson: () => current });
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/bookings': () => {
        current = bookedBallet();
        return { status: 201, body: bookingOf(nora.id) };
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    expect(section.getByText('12 places left')).toBeInTheDocument();
    // Without dependents there is nobody to choose.
    expect(section.queryByRole('combobox')).not.toBeInTheDocument();

    await user.click(await section.findByRole('button', { name: 'Book' }));

    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/bookings', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { personId: nora.id },
    });
    expect(await screen.findAllByText('Nora Example is booked.')).not.toHaveLength(0);
    expect(await section.findByText('11 places left')).toBeInTheDocument();
    expect(section.getByText('Booked')).toBeInTheDocument();
    expect(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    ).toBeEnabled();
    expect(section.queryByRole('button', { name: 'Book' })).not.toBeInTheDocument();
  });

  it('books a dependent chosen in the selector', async () => {
    mockMember({ lesson: () => bookedBallet(), fam: family });
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/bookings': { status: 201, body: bookingOf(lina.id) },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    // Nora is booked already: she has a cancel button and is not offered.
    expect(section.getByText('Nora Example')).toBeInTheDocument();
    const select = await section.findByRole('combobox', { name: 'Book for' });
    expect(select).toHaveTextContent('Lina Example');
    await user.click(select);
    expect(screen.queryByRole('option', { name: 'Nora Example (you)' })).not.toBeInTheDocument();
    await user.click(await screen.findByRole('option', { name: 'Max Example' }));
    await user.click(section.getByRole('button', { name: 'Book' }));

    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/bookings', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { personId: family.dependents[1]!.id },
    });
  });

  it('offers the signed-in member first', async () => {
    mockMember({ fam: family });

    const { section } = await openBooking();

    expect(await section.findByRole('combobox', { name: 'Book for' })).toHaveTextContent(
      'Nora Example (you)',
    );
  });

  it('says when the lesson is full and offers the waiting list instead of booking', async () => {
    mockMember({ lesson: () => ({ ...lessonBalletDetail, freePlaces: 0 }) });

    const { section } = await openBooking();

    expect(section.getByText('This lesson is full.')).toBeInTheDocument();
    expect(await section.findByRole('button', { name: 'Join the waiting list' })).toBeEnabled();
    expect(section.queryByRole('button', { name: 'Book' })).not.toBeInTheDocument();
    expect(
      section.getByRole('checkbox', { name: 'Stay on the list if I miss an invite' }),
    ).not.toBeChecked();
  });

  it.each([
    ['cutoff', 'Booking has closed.'],
    ['release', 'Not bookable yet.'],
    ['canceled', 'Canceled lessons cannot be booked.'],
    ['started', 'This lesson has started.'],
  ] as const)('shows why the API closed the lesson for booking (%s)', async (why, text) => {
    mockMember();
    mockApiPost({
      '/api/v1/lessons/{lessonId}/bookings': conflict('closed', 'closed', { why }),
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(await section.findByRole('button', { name: 'Book' }));

    expect(await section.findByText(text)).toBeInTheDocument();
    expect(section.queryByText('12 places left')).not.toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Book' })).toBeDisabled();
  });

  it('says until when the lesson can be booked, in school time', async () => {
    mockMember();

    const { section } = await openBooking();

    // An hour before the 18:00 start in Berlin, whatever the test's zone.
    expect(section.getByText('Bookable until Mon, Oct 26, 17:00')).toBeInTheDocument();
  });

  it('closes booking at the cutoff without asking the API', async () => {
    vi.setSystemTime(new Date('2026-10-26T16:00:00Z'));
    mockMember({ fam: family });
    const post = mockApiPost({});

    const { section } = await openBooking();

    expect(section.getByText('Booking has closed.')).toBeInTheDocument();
    expect(section.queryByText(/^Bookable until/)).not.toBeInTheDocument();
    expect(await section.findByRole('button', { name: 'Book' })).toBeDisabled();
    expect(post).not.toHaveBeenCalled();
  });

  it("follows the server's clock, not the device's", async () => {
    // The device thinks it is Tuesday, after the lesson; the server says
    // Friday, long before it.
    vi.setSystemTime(new Date('2026-10-27T12:00:00Z'));
    mockMember();

    const { section, queryClient } = await openBooking();
    queryClient.setQueryData(scheduleQuery({ from: '2026-10-26', to: '2026-11-09' }).queryKey, {
      ...schedule,
      now: '2026-10-23T10:00:00Z',
    });

    expect(await section.findByText('12 places left')).toBeInTheDocument();
    expect(section.queryByText('This lesson has started.')).not.toBeInTheDocument();
    expect(await section.findByRole('button', { name: 'Book' })).toBeEnabled();
  });

  it("closes booking by the server's clock while the device's is behind", async () => {
    mockMember();

    const { section, queryClient } = await openBooking();
    expect(await section.findByRole('button', { name: 'Book' })).toBeEnabled();
    queryClient.setQueryData(scheduleQuery({ from: '2026-10-26', to: '2026-11-09' }).queryKey, {
      ...schedule,
      now: '2026-10-26T16:30:00Z',
    });

    expect(await section.findByText('Booking has closed.')).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Book' })).toBeDisabled();
  });

  it('warns before a late cancellation', async () => {
    // Monday 11:00 in Berlin: past the cancellation cutoff, before the booking cutoff.
    vi.setSystemTime(new Date('2026-10-26T10:00:00Z'));
    mockMember({ lesson: () => bookedBallet() });
    const user = userEvent.setup();

    const { section } = await openBooking();
    expect(section.getByText(/^Bookable until/)).toBeInTheDocument();
    await user.click(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );

    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(within(confirm).getByText('This counts as a late cancellation.')).toBeInTheDocument();
  });

  it('does not warn before canceling a booking in good time', async () => {
    mockMember({ lesson: () => bookedBallet() });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );

    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(within(confirm).queryByText(/late cancellation/)).not.toBeInTheDocument();
  });

  it('never warns for a lesson the school canceled', async () => {
    vi.setSystemTime(new Date('2026-10-27T15:30:00Z'));
    mockMember({
      lesson: () => ({
        ...lessonContemporary,
        freePlaces: 9,
        bookedFor: [{ personId: nora.id, name: nora.name, bookingId }],
      }),
    });
    const user = userEvent.setup();

    const { section } = await openBooking(ids.lessonContemporary, 'Contemporary');
    await user.click(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );

    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(within(confirm).queryByText(/late cancellation/)).not.toBeInTheDocument();
  });

  it('keeps the bookings of a canceled lesson cancelable but offers no booking', async () => {
    mockMember({
      lesson: () => ({
        ...lessonContemporary,
        freePlaces: 9,
        bookedFor: [{ personId: nora.id, name: nora.name, bookingId }],
      }),
    });

    const { section } = await openBooking(ids.lessonContemporary, 'Contemporary');

    expect(section.getByText('Canceled lessons cannot be booked.')).toBeInTheDocument();
    expect(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    ).toBeEnabled();
    expect(section.queryByRole('button', { name: 'Book' })).not.toBeInTheDocument();
  });

  it('neither books nor cancels once the lesson has started', async () => {
    vi.setSystemTime(new Date('2026-10-26T17:30:00Z'));
    mockMember({ lesson: () => bookedBallet() });

    const { section } = await openBooking();

    expect(section.getByText('This lesson has started.')).toBeInTheDocument();
    expect(await section.findByText('Nora Example')).toBeInTheDocument();
    expect(section.queryByRole('button', { name: /Cancel/ })).not.toBeInTheDocument();
    expect(section.queryByRole('button', { name: 'Book' })).not.toBeInTheDocument();
  });

  it('shows the booking at once and rolls it back when the API refuses', async () => {
    mockMember();
    const answer = deferred<unknown>();
    const post = vi.spyOn(api, 'POST').mockImplementation(
      // The 409 comes once the test has seen the optimistic booking.
      (() => answer.promise) as unknown as typeof api.POST,
    );
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(await section.findByRole('button', { name: 'Book' }));

    expect(await section.findByText('11 places left')).toBeInTheDocument();
    expect(section.getByText('Booked')).toBeInTheDocument();
    // Until the API answers, the new booking has no ID to cancel.
    expect(
      section.getByRole('button', { name: 'Cancel the booking of Nora Example' }),
    ).toBeDisabled();

    const refused = conflict('already-booked');
    answer.resolve({
      data: undefined,
      error: refused.body,
      response: new Response(null, { status: 409 }),
    });

    expect(await section.findByText('12 places left')).toBeInTheDocument();
    expect(section.queryByText('Booked')).not.toBeInTheDocument();
    expect(
      await screen.findAllByText('Nora Example is booked for this lesson already.'),
    ).not.toHaveLength(0);
    expect(post).toHaveBeenCalledTimes(1);
  });

  it('says when the lesson is gone', async () => {
    mockMember();
    mockApiPost({ '/api/v1/lessons/{lessonId}/bookings': problem(404, 'not found') });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(await section.findByRole('button', { name: 'Book' }));

    expect(
      await screen.findAllByText('This lesson or booking is not available any more.'),
    ).not.toHaveLength(0);
  });

  it('cancels a booking after asking', async () => {
    let current: LessonDetail = bookedBallet();
    mockMember({ lesson: () => current });
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/cancel': () => {
        current = lessonBalletDetail;
        return { body: bookingOf(nora.id, { status: 'canceled' }) };
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );
    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(
      within(confirm).getByText(
        'Nora Example will no longer be booked for Ballet, Monday, October 26, 18:00–19:30.',
      ),
    ).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();

    await user.click(within(confirm).getByRole('button', { name: 'Cancel booking' }));

    await waitFor(() => expect(confirm).not.toBeInTheDocument());
    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/cancel', {
      params: { path: { bookingId } },
    });
    expect(
      await screen.findAllByText('The booking of Nora Example has been canceled.'),
    ).not.toHaveLength(0);
    expect(await section.findByText('12 places left')).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Book' })).toBeEnabled();
  });

  it('says when a cancellation was late', async () => {
    mockMember({ lesson: () => bookedBallet() });
    mockApiPost({
      '/api/v1/bookings/{bookingId}/cancel': {
        body: bookingOf(nora.id, { status: 'canceled', late: true }),
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );
    await user.click(screen.getByRole('button', { name: 'Cancel booking' }));

    expect(await screen.findAllByText('It was a late cancellation.')).not.toHaveLength(0);
  });

  it('keeps the confirmation open when the booking was canceled already', async () => {
    mockMember({ lesson: () => bookedBallet() });
    mockApiPost({ '/api/v1/bookings/{bookingId}/cancel': conflict('already-canceled') });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    );
    const confirm = screen.getByRole('alertdialog', { name: 'Cancel this booking?' });
    await user.click(within(confirm).getByRole('button', { name: 'Cancel booking' }));

    expect(
      await within(confirm).findByText('This booking was canceled already.'),
    ).toBeInTheDocument();
  });

  it('cancels the following weeks of a class after showing them', async () => {
    mockMember({ lesson: () => bookedBallet() });
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/cancel-following': {
        body: {
          bookings: [
            {
              bookingId,
              lessonId: ids.lessonBallet,
              startsAt: '2026-10-26T17:00:00Z',
              endsAt: '2026-10-26T18:30:00Z',
              late: true,
            },
            {
              bookingId: '0190a000-0000-7000-8000-000000000602',
              lessonId: '0190a000-0000-7000-8000-000000000112',
              startsAt: '2026-11-02T17:00:00Z',
              endsAt: '2026-11-02T18:30:00Z',
              late: false,
            },
          ],
        },
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(
      await section.findByRole('button', {
        name: 'Also cancel the following weeks of Nora Example',
      }),
    );

    const dialog = screen.getByRole('alertdialog', { name: 'Cancel the following weeks too?' });
    const list = await within(dialog).findByRole('list');
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(3);
    // School dates and times (Berlin), whatever the test's zone.
    expect(items[0]).toHaveTextContent('Monday, October 2618:00–19:30Late');
    expect(items[1]).toHaveTextContent('Monday, November 218:00–19:30');
    expect(items[1]).not.toHaveTextContent('Late');
    expect(post).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole('button', { name: 'Cancel 3 bookings' }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/cancel-following', {
      params: { path: { bookingId } },
      body: { until: '2027-07-31' },
    });
    // The toast counts what the API really canceled.
    expect(await screen.findAllByText('2 bookings have been canceled.')).not.toHaveLength(0);
    expect(await screen.findAllByText('1 of them was a late cancellation.')).not.toHaveLength(0);
  });

  it('cancels the following weeks up to a date and says when a member is booked again', async () => {
    const get = mockMember({ lesson: () => bookedBallet() });
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/cancel-following': {
        body: {
          bookings: followingBookings.slice(0, 2),
          until: '2026-11-02',
          pausedUntil: '2026-11-02',
        },
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(
      await section.findByRole('button', {
        name: 'Also cancel the following weeks of Nora Example',
      }),
    );

    const dialog = within(
      screen.getByRole('alertdialog', { name: 'Cancel the following weeks too?' }),
    );
    // The field starts with the default the API names: the end of the term.
    const until = dialog.getByLabelText('Cancel up to and including');
    await waitFor(() => expect(until).toHaveValue('2027-07-31'));
    expect(
      dialog.getByText(
        'Nora Example stays a member of the class and is booked again automatically from Sunday, August 1, 2027.',
      ),
    ).toBeInTheDocument();

    // A date input takes the whole date at once.
    fireEvent.change(until, { target: { value: '2026-11-02' } });
    await waitFor(() => expect(dialog.getAllByRole('listitem')).toHaveLength(2));
    expect(get).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/cancel-following', {
      params: { path: { bookingId }, query: { until: '2026-11-02' } },
      signal: expect.any(AbortSignal),
    });
    expect(
      dialog.getByText(
        'Nora Example stays a member of the class and is booked again automatically from Tuesday, November 3, 2026.',
      ),
    ).toBeInTheDocument();

    await user.click(dialog.getByRole('button', { name: 'Cancel 2 bookings' }));
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/cancel-following', {
        params: { path: { bookingId } },
        body: { until: '2026-11-02' },
      }),
    );
  });

  it('offers no following weeks for a one-off lesson', async () => {
    mockMember({
      lesson: () => {
        const { classId: _, ...oneOff } = bookedBallet();
        return oneOff;
      },
    });

    const { section } = await openBooking();

    expect(
      await section.findByRole('button', { name: 'Cancel the booking of Nora Example' }),
    ).toBeInTheDocument();
    expect(section.queryByRole('button', { name: /following weeks/ })).not.toBeInTheDocument();
  });

  it('asks a visitor to sign in to book', async () => {
    mockMember({ me: null });

    const { section } = await openBooking();

    expect(section.getByText('12 places left')).toBeInTheDocument();
    expect(await section.findByRole('link', { name: 'Sign in to book' })).toHaveAttribute(
      'href',
      '/login',
    );
    expect(section.queryByRole('button', { name: 'Book' })).not.toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockMember({ lesson: () => bookedBallet(), fam: family });
    await i18n.changeLanguage('de');

    const { section } = await openBooking(ids.lessonBallet, 'Ballet', 'Buchung');

    expect(section.getByText('Noch 11 Plätze frei')).toBeInTheDocument();
    expect(await section.findByRole('combobox', { name: 'Buchen für' })).toBeInTheDocument();
    expect(
      section.getByRole('button', { name: 'Buchung von Nora Example stornieren' }),
    ).toBeInTheDocument();
  });
});

describe('BookingSection waiting list', () => {
  const entryId = '0190a000-0000-7000-8000-000000000701';
  const full = { ...lessonBalletDetail, freePlaces: 0 };
  /** The full ballet lesson with `entries` of the family on its waiting list. */
  const waitingBallet = (...entries: WaitlistFor[]): LessonDetail => ({
    ...full,
    waitlist: entries,
  });
  const noraWaiting = (position = 3): WaitlistFor => ({
    personId: nora.id,
    name: nora.name,
    entryId,
    status: 'waiting',
    position,
    stayOnList: true,
  });
  // Friday 22:00 in Berlin, ten hours after the fixtures' "now".
  const invitedUntil = '2026-10-23T20:00:00Z';
  const invitedOf = (who: { id: string; name: string }, stayOnList = false): WaitlistFor => ({
    personId: who.id,
    name: who.name,
    entryId,
    status: 'invited',
    expiresAt: invitedUntil,
    stayOnList,
  });

  it('joins the waiting list of a full lesson, staying on it, and shows the position', async () => {
    let current: LessonDetail = full;
    mockMember({ lesson: () => current });
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/waitlist': () => {
        current = waitingBallet(noraWaiting(3));
        return {
          status: 201,
          body: {
            id: entryId,
            lessonId: ids.lessonBallet,
            personId: nora.id,
            status: 'waiting',
            stayOnList: true,
            joinedAt: '2026-10-23T10:00:00Z',
            position: 3,
          },
        };
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(
      section.getByRole('checkbox', { name: 'Stay on the list if I miss an invite' }),
    );
    await user.click(await section.findByRole('button', { name: 'Join the waiting list' }));

    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/waitlist', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { personId: nora.id, stayOnList: true },
    });
    expect(
      await screen.findAllByText('Nora Example is on the waiting list, position 3.'),
    ).not.toHaveLength(0);
    expect(await section.findByText('Waiting, position 3')).toBeInTheDocument();
    expect(
      section.getByRole('button', { name: 'Leave the waiting list for Nora Example' }),
    ).toBeEnabled();
    // Nobody else to put on the list.
    expect(
      section.queryByRole('button', { name: 'Join the waiting list' }),
    ).not.toBeInTheDocument();
  });

  it('joins without staying on the list by default, for the chosen dependent', async () => {
    mockMember({ lesson: () => full, fam: family });
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/waitlist': {
        status: 201,
        body: {
          id: entryId,
          lessonId: ids.lessonBallet,
          personId: lina.id,
          status: 'waiting',
          stayOnList: false,
          joinedAt: '2026-10-23T10:00:00Z',
          position: 1,
        },
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    const select = await section.findByRole('combobox', { name: 'Join for' });
    await user.click(select);
    await user.click(await screen.findByRole('option', { name: 'Lina Example' }));
    await user.click(section.getByRole('button', { name: 'Join the waiting list' }));

    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/waitlist', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { personId: lina.id, stayOnList: false },
    });
    expect(
      await screen.findAllByText('Lina Example is on the waiting list, position 1.'),
    ).not.toHaveLength(0);
  });

  it('says why joining failed', async () => {
    mockMember({ lesson: () => full });
    mockApiPost({ '/api/v1/lessons/{lessonId}/waitlist': conflict('already-waiting') });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(await section.findByRole('button', { name: 'Join the waiting list' }));

    expect(
      await screen.findAllByText('Nora Example is on the waiting list already.'),
    ).not.toHaveLength(0);
  });

  it('shows a closed lesson instead of the waiting list when the API says so', async () => {
    mockMember({ lesson: () => full });
    mockApiPost({
      '/api/v1/lessons/{lessonId}/waitlist': conflict('closed', 'closed', { why: 'release' }),
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(await section.findByRole('button', { name: 'Join the waiting list' }));

    expect(await section.findByText('Not bookable yet.')).toBeInTheDocument();
    expect(
      section.queryByRole('button', { name: 'Join the waiting list' }),
    ).not.toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Book' })).toBeDisabled();
  });

  it('leaves the waiting list', async () => {
    let current = waitingBallet(noraWaiting(2));
    mockMember({ lesson: () => current });
    const del = mockApiDelete({
      '/api/v1/waitlist/{entryId}': () => {
        current = full;
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    expect(section.getByText('Waiting, position 2')).toBeInTheDocument();
    await user.click(
      section.getByRole('button', { name: 'Leave the waiting list for Nora Example' }),
    );

    expect(del).toHaveBeenCalledWith('/api/v1/waitlist/{entryId}', {
      params: { path: { entryId } },
    });
    expect(await screen.findAllByText('Nora Example has left the waiting list.')).not.toHaveLength(
      0,
    );
    expect(await section.findByRole('button', { name: 'Join the waiting list' })).toBeEnabled();
    expect(section.queryByText('Waiting, position 2')).not.toBeInTheDocument();
  });

  it('accepts an invite, which books the place', async () => {
    let current = waitingBallet(invitedOf(nora));
    mockMember({ lesson: () => current });
    const post = mockApiPost({
      '/api/v1/waitlist/{entryId}/accept': () => {
        current = bookedBallet();
        return { body: bookingOf(nora.id) };
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    expect(section.getByText('Invited')).toBeInTheDocument();
    // The expiry in school time, whatever the test's zone.
    expect(
      section.getByText('A place is reserved for you until Fri, Oct 23, 22:00.'),
    ).toBeInTheDocument();
    // The person is not offered for booking or joining again.
    expect(
      section.queryByRole('button', { name: 'Join the waiting list' }),
    ).not.toBeInTheDocument();
    await user.click(section.getByRole('button', { name: 'Accept the place for Nora Example' }));

    expect(post).toHaveBeenCalledWith('/api/v1/waitlist/{entryId}/accept', {
      params: { path: { entryId } },
    });
    expect(await screen.findAllByText('Nora Example is booked.')).not.toHaveLength(0);
    expect(await section.findByText('Booked')).toBeInTheDocument();
    expect(section.queryByText('Invited')).not.toBeInTheDocument();
  });

  it('shows the invite of a dependent by name', async () => {
    mockMember({ lesson: () => waitingBallet(invitedOf(lina)), fam: family });

    const { section } = await openBooking();

    expect(
      await section.findByText('A place is reserved for Lina Example until Fri, Oct 23, 22:00.'),
    ).toBeInTheDocument();
    expect(
      section.getByRole('button', { name: 'Decline the place for Lina Example' }),
    ).toBeEnabled();
  });

  it('declines an invite and says when the person is back on the list', async () => {
    let current = waitingBallet(invitedOf(nora, true));
    mockMember({ lesson: () => current });
    const post = mockApiPost({
      '/api/v1/waitlist/{entryId}/decline': () => {
        current = waitingBallet(noraWaiting(2));
        return {
          body: {
            id: entryId,
            lessonId: ids.lessonBallet,
            personId: nora.id,
            status: 'waiting',
            stayOnList: true,
            joinedAt: '2026-10-23T10:00:00Z',
            position: 2,
          },
        };
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(section.getByRole('button', { name: 'Decline the place for Nora Example' }));

    expect(post).toHaveBeenCalledWith('/api/v1/waitlist/{entryId}/decline', {
      params: { path: { entryId } },
    });
    expect(
      await screen.findAllByText('Nora Example is back on the waiting list, position 2.'),
    ).not.toHaveLength(0);
    expect(await section.findByText('Waiting, position 2')).toBeInTheDocument();
  });

  it('says when an invite ran out while accepting', async () => {
    mockMember({ lesson: () => waitingBallet(invitedOf(nora)) });
    mockApiPost({ '/api/v1/waitlist/{entryId}/accept': conflict('invite-expired') });
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(section.getByRole('button', { name: 'Accept the place for Nora Example' }));

    expect(await screen.findAllByText('The invite has run out.')).not.toHaveLength(0);
  });

  it("shows an invite as expired once the server's clock passes it", async () => {
    // Friday 23:00 in Berlin: an hour after the invite ran out.
    vi.setSystemTime(new Date('2026-10-23T21:00:00Z'));
    mockMember({ lesson: () => waitingBallet(invitedOf(nora, true)) });

    const { section } = await openBooking();

    expect(section.getByText('Invite expired')).toBeInTheDocument();
    expect(
      section.getByText('The invite has run out; Nora Example goes back to the end of the list.'),
    ).toBeInTheDocument();
    expect(
      section.queryByRole('button', { name: 'Accept the place for Nora Example' }),
    ).not.toBeInTheDocument();
  });

  it('offers a person again whose invite ran out without staying on the list', async () => {
    mockMember({
      lesson: () => waitingBallet({ ...invitedOf(nora), status: 'expired', expiresAt: undefined }),
    });

    const { section } = await openBooking();

    expect(section.getByText('Invite expired')).toBeInTheDocument();
    expect(section.getByText('The invite has run out.')).toBeInTheDocument();
    expect(await section.findByRole('button', { name: 'Join the waiting list' })).toBeEnabled();
  });

  it("offers a person again once an ending invite passes its expiry by the server's clock", async () => {
    vi.setSystemTime(new Date('2026-10-23T21:00:00Z'));
    mockMember({ lesson: () => waitingBallet(invitedOf(nora)) });

    const { section } = await openBooking();

    expect(await section.findByRole('button', { name: 'Join the waiting list' })).toBeEnabled();
  });

  it('keeps a person who stays on the list from joining again after a missed invite', async () => {
    mockMember({
      lesson: () => waitingBallet({ ...invitedOf(nora, true), status: 'expired' }),
    });

    const { section } = await openBooking();

    expect(section.getByText('Invite expired')).toBeInTheDocument();
    expect(
      section.queryByRole('button', { name: 'Join the waiting list' }),
    ).not.toBeInTheDocument();
  });

  it('shows an invite without an expiry', async () => {
    mockMember({ lesson: () => waitingBallet({ ...invitedOf(nora), expiresAt: undefined }) });

    const { section } = await openBooking();

    expect(await section.findByText('A place is reserved for you.')).toBeInTheDocument();
  });

  it('starts "stay on the list" unchecked for each person and after a join', async () => {
    let current: LessonDetail = full;
    mockMember({ lesson: () => current, fam: family });
    mockApiPost({
      '/api/v1/lessons/{lessonId}/waitlist': () => {
        current = waitingBallet(noraWaiting(1));
        return {
          status: 201,
          body: {
            id: entryId,
            lessonId: ids.lessonBallet,
            personId: nora.id,
            status: 'waiting',
            stayOnList: true,
            joinedAt: '2026-10-23T10:00:00Z',
            position: 1,
          },
        };
      },
    });
    const user = userEvent.setup();

    const { section } = await openBooking();
    const stay = () =>
      section.getByRole('checkbox', { name: 'Stay on the list if I miss an invite' });
    await user.click(stay());
    expect(stay()).toBeChecked();
    // Another person: unchecked.
    await user.click(await section.findByRole('combobox', { name: 'Join for' }));
    await user.click(await screen.findByRole('option', { name: 'Lina Example' }));
    expect(stay()).not.toBeChecked();
    // Back to Nora, checked again, and joined: the next person starts unchecked.
    await user.click(section.getByRole('combobox', { name: 'Join for' }));
    await user.click(await screen.findByRole('option', { name: 'Nora Example (you)' }));
    expect(stay()).toBeChecked();
    await user.click(section.getByRole('button', { name: 'Join the waiting list' }));

    expect(await section.findByText('Waiting, position 1')).toBeInTheDocument();
    expect(section.getByRole('combobox', { name: 'Join for' })).toHaveTextContent('Lina Example');
    expect(stay()).not.toBeChecked();
  });

  it('does not turn a running booking of the last place into a join', async () => {
    mockMember({ lesson: () => ({ ...lessonBalletDetail, freePlaces: 1 }), fam: family });
    const answer = deferred<unknown>();
    vi.spyOn(api, 'POST').mockImplementation((() => answer.promise) as unknown as typeof api.POST);
    const user = userEvent.setup();

    const { section } = await openBooking();
    await user.click(await section.findByRole('button', { name: 'Book' }));

    // Nora takes the last place for now; Lina is next, but nothing is offered meanwhile.
    expect(await section.findByText('Booked')).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Booking…' })).toBeDisabled();
    expect(
      section.queryByRole('button', { name: 'Join the waiting list' }),
    ).not.toBeInTheDocument();
  });

  it('offers nothing on the waiting list once the lesson has started', async () => {
    vi.setSystemTime(new Date('2026-10-26T17:30:00Z'));
    mockMember({ lesson: () => waitingBallet(noraWaiting(1)) });

    const { section } = await openBooking();

    expect(section.getByText('Waiting, position 1')).toBeInTheDocument();
    expect(section.queryByRole('button', { name: /waiting list/ })).not.toBeInTheDocument();
  });
});
