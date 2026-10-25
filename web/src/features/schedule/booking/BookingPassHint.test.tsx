import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import type { MyLessonBooking } from '@/api/queries';
import {
  family,
  ids,
  lessonBalletDetail,
  meMember,
  mockApiGet,
  myPasses,
  myPassesAnswer,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { LessonSheet } from '../detail/LessonSheet';

// Friday 2026-10-23 12:00 in Berlin; the ballet lesson is on Monday 2026-10-26.
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-23T10:00:00Z'));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function mockMember(
  passes: ReturnType<typeof myPassesAnswer> | ReturnType<typeof problem> = myPassesAnswer(
    'attendance',
  ),
  lesson = lessonBalletDetail,
  upcoming: MyLessonBooking[] = [],
) {
  return mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: meMember },
    '/api/v1/family': { body: family },
    '/api/v1/lessons/{lessonId}': { body: lesson },
    '/api/v1/my/passes': passes,
    '/api/v1/my/lessons': {
      body: { timezone: 'Europe/Berlin', now: '2026-10-23T10:00:00Z', upcoming, waitlist: [] },
    },
  });
}

/** A coming booking of Nora's, its attendance not taken yet, on `startsAt`. */
const pending = (n: number, startsAt: string): MyLessonBooking => {
  const {
    bookingClosesAt: _closes,
    lateCancelFrom: _late,
    freePlaces: _free,
    bookedFor: _booked,
    waitlist: _waitlist,
    courseTypeDescription: _description,
    locationAddress: _address,
    ...summary
  } = lessonBalletDetail;
  return {
    booking: {
      id: `0190a000-0000-7000-8000-00000000065${n}`,
      lessonId: `0190a000-0000-7000-8000-00000000015${n}`,
      personId: meMember.personId,
      status: 'booked',
      source: 'self',
      bookedAt: '2026-10-20T10:00:00Z',
      late: false,
    },
    personName: 'Nora Example',
    lesson: {
      ...summary,
      id: `0190a000-0000-7000-8000-00000000015${n}`,
      startsAt,
      endsAt: startsAt.replace('T17:', 'T18:'),
    },
    canCancel: true,
    payment: { status: 'pending' },
  };
};

/** Opens the ballet lesson's sheet and returns its booking section. */
async function openBooking(name = 'Ballet', region = 'Booking') {
  render(<Toaster />);
  renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });
  const dialog = await screen.findByRole('dialog', { name });
  return within(await within(dialog).findByRole('region', { name: region }));
}

/** Picks whom to book for. */
async function bookFor(
  user: ReturnType<typeof userEvent.setup>,
  section: ReturnType<typeof within>,
  name: string,
) {
  await user.click(await section.findByRole('combobox', { name: 'Book for' }));
  await user.click(await screen.findByRole('option', { name }));
}

describe('the pass hint of a booking', () => {
  it('says which pass a booking will use on attendance', async () => {
    mockMember();
    const user = userEvent.setup();

    const section = await openBooking();
    const book = await section.findByRole('button', { name: 'Book' });
    expect(
      await section.findByText('Will use 10-visit pass when attended; 6 visits left after that.'),
    ).toBeInTheDocument();
    expect(book).toHaveAccessibleDescription(
      'Will use 10-visit pass when attended; 6 visits left after that.',
    );

    // A time pass comes first, and it has no visits to count.
    await bookFor(user, section, 'Lina Example');
    expect(await section.findByText('Will use 30-day pass when attended.')).toBeInTheDocument();
    expect(book).toBeEnabled();
  });

  it('says which pass a booking uses on booking', async () => {
    const get = mockMember(myPassesAnswer('booking'));
    const user = userEvent.setup();

    const section = await openBooking();
    expect(
      await section.findByText('Uses 10-visit pass; 6 visits left after this booking.'),
    ).toBeInTheDocument();
    await bookFor(user, section, 'Lina Example');
    expect(await section.findByText('Uses 30-day pass.')).toBeInTheDocument();
    // On booking, no booking waits to use a visit.
    expect(get).not.toHaveBeenCalledWith('/api/v1/my/lessons', expect.anything());
  });

  it('says to pay at the desk without a valid pass, and still books', async () => {
    mockMember();
    const user = userEvent.setup();

    const section = await openBooking();
    await bookFor(user, section, 'Max Example');

    expect(
      await section.findByText('No valid pass for this day — pay at the desk.'),
    ).toBeInTheDocument();
    expect(section.getByRole('button', { name: 'Book' })).toBeEnabled();
  });

  it('does not count a pass that has expired by the lesson day', async () => {
    const lesson = {
      ...lessonBalletDetail,
      startsAt: '2026-11-09T17:00:00Z',
      endsAt: '2026-11-09T18:30:00Z',
      bookingClosesAt: '2026-11-09T16:00:00Z',
      lateCancelFrom: '2026-11-08T17:00:00Z',
    };
    mockMember(myPassesAnswer('attendance'), lesson);
    const user = userEvent.setup();

    const section = await openBooking();
    // Lina's 30-day pass ends on November 8.
    await bookFor(user, section, 'Lina Example');
    expect(
      await section.findByText('No valid pass for this day — pay at the desk.'),
    ).toBeInTheDocument();
  });

  it('warns when bookings not attended yet would use the visits left first', async () => {
    const passes = myPassesAnswer(
      'attendance',
      myPasses.map((p, i) => (i === 0 ? { ...p, visitsRemaining: 2 } : p)),
    );
    mockMember(passes, lessonBalletDetail, [
      pending(1, '2026-10-24T17:00:00Z'),
      pending(2, '2026-10-25T17:00:00Z'),
    ]);

    const section = await openBooking();

    const warning =
      'Will use 10-visit pass when attended, but its visits left (2) are already planned for 2 other bookings — pay at the desk if it runs out.';
    expect(await section.findByText(warning)).toBeInTheDocument();
    expect(await section.findByRole('button', { name: 'Book' })).toHaveAccessibleDescription(
      warning,
    );
  });

  it('keeps the usual words while the visits left suffice', async () => {
    const passes = myPassesAnswer(
      'attendance',
      myPasses.map((p, i) => (i === 0 ? { ...p, visitsRemaining: 2 } : p)),
    );
    mockMember(passes, lessonBalletDetail, [pending(1, '2026-10-24T17:00:00Z')]);

    const section = await openBooking();

    expect(
      // One visit for the other booking, one for this one.
      await section.findByText('Will use 10-visit pass when attended; 0 visits left after that.'),
    ).toBeInTheDocument();
  });

  it('says nothing about passes it could not load', async () => {
    mockMember(problem(500, 'boom'));

    const section = await openBooking();

    const book = await section.findByRole('button', { name: 'Book' });
    expect(book).not.toHaveAttribute('aria-describedby');
    expect(section.queryByText(/pass/)).not.toBeInTheDocument();
  });

  it('shows no hint for a full lesson', async () => {
    mockMember(myPassesAnswer('attendance'), { ...lessonBalletDetail, freePlaces: 0 });

    const section = await openBooking();

    expect(
      await section.findByRole('button', { name: 'Join the waiting list' }),
    ).toBeInTheDocument();
    expect(section.queryByText(/10-visit pass/)).not.toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockMember();
    await i18n.changeLanguage('de');

    const section = await openBooking('Ballet', 'Buchung');

    expect(
      await section.findByText(
        'Nutzt 10-visit pass, sobald du anwesend warst; danach bleiben 6 Besuche übrig.',
      ),
    ).toBeInTheDocument();
  });
});
