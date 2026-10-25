import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ScheduleLesson } from '@/api/client';
import type { BookingPayment, MyLessonBooking, MyLessons, Receipt } from '@/api/queries';
import { openDownload } from '@/api/download';
import i18n from '@/i18n';
import {
  family,
  lessonBallet,
  lessonWorkshop,
  meMember,
  mockApiGet,
  myPasses,
  myPassesAnswer,
  problem,
  publicSettings,
  receiptCanceled,
  receiptCancellation,
  receiptSale,
} from '@/test/mockApi';
import { renderAtMyLessons } from '@/test/utils';
import { MyLessonsPage } from './MyLessonsPage';

vi.mock('@/api/download', () => ({ openDownload: vi.fn(() => Promise.resolve()) }));

// Friday 2026-10-23 12:00 in Berlin, as the passes fixture's today.
const now = '2026-10-23T10:00:00Z';

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date(now));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

/** A coming booking of Nora's for `lesson`, paid as `payment` says. */
const upcoming = (n: number, lesson: ScheduleLesson, payment: BookingPayment): MyLessonBooking => {
  const {
    bookingClosesAt: _closes,
    lateCancelFrom: _late,
    freePlaces: _free,
    bookedFor: _booked,
    waitlist: _waitlist,
    ...summary
  } = lesson;
  return {
    booking: {
      id: id(600 + n),
      lessonId: lesson.id,
      personId: meMember.personId,
      status: 'booked',
      source: 'self',
      bookedAt: '2026-10-20T10:00:00Z',
      late: false,
    },
    personName: 'Nora Example',
    lesson: summary,
    canCancel: true,
    payment,
  };
};

const lessons: MyLessons = {
  timezone: 'Europe/Berlin',
  now,
  upcoming: [
    upcoming(1, lessonBallet, {
      status: 'paid',
      pass: { id: myPasses[0]!.id, name: '10-visit pass', kind: 'visits', visitsRemaining: 6 },
    }),
    upcoming(2, lessonWorkshop, { status: 'unpaid' }),
  ],
  waitlist: [],
};

function mockMember(
  passes: ReturnType<typeof myPassesAnswer> | ReturnType<typeof problem>,
  receipts: Receipt[] = [],
) {
  return mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: meMember },
    '/api/v1/family': { body: family },
    '/api/v1/my/lessons': { body: lessons },
    '/api/v1/my/lessons/history': {
      body: {
        timezone: 'Europe/Berlin',
        now,
        before: now,
        nextBefore: '2026-07-23T10:00:00Z',
        hasMore: false,
        items: [],
      },
    },
    '/api/v1/my/passes': passes,
    '/api/v1/my/receipts': { body: { items: receipts } },
  });
}

/** The card of a pass in "My passes", named by its type and owner. */
async function card(name: RegExp) {
  const region = within(await screen.findByRole('region', { name: 'My passes' }));
  return within(await region.findByRole('listitem', { name }));
}

describe('My passes', () => {
  it("shows the family's passes with their visits, days, state and payer", async () => {
    mockMember(myPassesAnswer());

    renderAtMyLessons(<MyLessonsPage />);

    const nora = await card(/^10-visit pass, Nora Example$/);
    expect(nora.getByText('Usable')).toBeInTheDocument();
    expect(nora.getByText('7 of 10 visits left')).toBeInTheDocument();
    expect(nora.getByText('Oct 1 – Mar 29, 2027')).toBeInTheDocument();
    const uses = within(nora.getByRole('list', { name: 'Uses of 10-visit pass' }));
    const [first, second] = uses.getAllByRole('listitem');
    expect(first).toHaveTextContent('BalletMon, Oct 19, 19:00· Nora Example');
    expect(within(second!).getByText('No-show')).toBeInTheDocument();
    expect(nora.getByText('3 uses in total')).toBeInTheDocument();

    const lina = await card(/^30-day pass, Lina Example$/);
    expect(lina.getByText('Unlimited visits')).toBeInTheDocument();
    expect(lina.getByText('Paid by Nora Example')).toBeInTheDocument();
    expect(lina.getByText('14 uses in total')).toBeInTheDocument();

    const region = within(screen.getByRole('region', { name: 'My passes' }));
    const [usedUp, voided] = region
      .getAllByRole('listitem', { name: 'Single visit, Nora Example' })
      .map((li) => within(li));
    const single = usedUp!;
    expect(single.getByText('Used up')).toBeInTheDocument();
    expect(single.getByText('0 of 1 visits left')).toBeInTheDocument();
    expect(single.getByText('From Sep 20, no expiry')).toBeInTheDocument();
    expect(single.getByText('Canceled late')).toBeInTheDocument();

    const maxPass = await card(/^10-visit pass, Max Example$/);
    expect(maxPass.getByText('Expired')).toBeInTheDocument();
    expect(maxPass.getByText('Given by the school')).toBeInTheDocument();

    expect(voided!.getByText('Voided')).toBeInTheDocument();
    expect(voided!.getByText('Not used yet.')).toBeInTheDocument();
  });

  it('is left out for a family without passes', async () => {
    mockMember(myPassesAnswer('attendance', []));

    renderAtMyLessons(<MyLessonsPage />);

    expect(await screen.findByRole('region', { name: 'History' })).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole('region', { name: 'My passes' })).not.toBeInTheDocument(),
    );
    expect(screen.queryByText('Loading your lessons…')).not.toBeInTheDocument();
  });

  it('says when the passes cannot be loaded', async () => {
    mockMember(problem(500, 'boom'));

    renderAtMyLessons(<MyLessonsPage />);

    expect(
      await within(await screen.findByRole('region', { name: 'My passes' })).findByText(
        'Your passes could not be loaded.',
      ),
    ).toBeInTheDocument();
  });

  it('marks the coming lessons as paid or unpaid', async () => {
    mockMember(myPassesAnswer());

    renderAtMyLessons(<MyLessonsPage />);

    const coming = within(await screen.findByRole('region', { name: 'Coming up' }));
    const [ballet, workshop] = await coming.findAllByRole('listitem');
    expect(within(ballet!).getByText('Paid')).toBeInTheDocument();
    expect(within(workshop!).getByText('Unpaid')).toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockMember(myPassesAnswer());
    await i18n.changeLanguage('de');

    renderAtMyLessons(<MyLessonsPage />);

    const region = within(await screen.findByRole('region', { name: 'Meine Karten' }));
    expect((await region.findAllByText('Gültig')).length).toBeGreaterThan(0);
    expect(region.getByText('7 von 10 Besuchen übrig')).toBeInTheDocument();
    expect(region.getByText('Aufgebraucht')).toBeInTheDocument();
  });
});

describe('My receipts', () => {
  it('lists the receipts the member paid, each with its PDF', async () => {
    mockMember(myPassesAnswer(), [receiptCancellation, receiptSale, receiptCanceled]);
    const user = userEvent.setup();

    renderAtMyLessons(<MyLessonsPage />);

    const region = within(await screen.findByRole('region', { name: 'My receipts' }));
    const rows = (await region.findAllByRole('listitem')).map((li) => within(li));
    expect(rows[0]!.getByText('Cancellation')).toBeInTheDocument();
    expect(rows[1]!.getByText('SD-2026-0002')).toBeInTheDocument();
    expect(
      rows[1]!.getByText('Oct 5, 2026 · 10-visit pass for Noah Sample · €140.00'),
    ).toBeInTheDocument();
    expect(rows[2]!.getByText('Canceled')).toBeInTheDocument();

    await user.click(region.getByRole('button', { name: 'Download receipt SD-2026-0002 as PDF' }));
    expect(openDownload).toHaveBeenCalledWith(`/api/v1/my/receipts/${receiptSale.id}/pdf`);
  });

  it('is left out for someone who paid for nothing', async () => {
    mockMember(myPassesAnswer());

    renderAtMyLessons(<MyLessonsPage />);

    expect(await screen.findByRole('region', { name: 'My passes' })).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole('region', { name: 'My receipts' })).not.toBeInTheDocument(),
    );
  });
});
