import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, type ScheduleLesson } from '@/api/client';
import { familyQuery, meQuery, type WaitlistFor } from '@/api/queries';
import type { QueryClient } from '@tanstack/react-query';
import { Toaster } from '@/components/ui/sonner';
import {
  conflict,
  family,
  ids,
  lessonBallet,
  lessonContemporary,
  meMember,
  type MockRequest,
  mockApiGet,
  mockApiPost,
  signedOut,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { useLessonQuickAction } from './quickAction';

afterEach(() => {
  vi.restoreAllMocks();
});

// Friday 2026-10-23 12:00 in Berlin, before every fixture lesson (the
// ballet lesson: Monday 18:00 Berlin, booking closes at 17:00, a
// cancellation is late from Sunday 18:00 on).
const friday = Date.parse('2026-10-23T10:00:00Z');
const bookingId = '0190a000-0000-7000-8000-000000000601';
const alone = { ...family, dependents: [] };
const nora = { personId: meMember.personId, name: 'Nora Example', bookingId };

/** Shows the quick action's kind, its person, and a button that runs it. */
function Probe({ lesson, now }: { lesson: ScheduleLesson; now: number }) {
  const { action, dialog } = useLessonQuickAction(lesson, 'Europe/Berlin', now);
  return (
    <>
      <p data-testid="kind">{action.kind}</p>
      {'run' in action && (
        <button type="button" disabled={action.pending} onClick={action.run}>
          run for {action.personId}
        </button>
      )}
      {dialog}
    </>
  );
}

/** The API of the member Nora with `fam` as her family (a guest without `me`). */
function mockMember({ fam = alone, me = true }: { fam?: typeof family; me?: boolean } = {}) {
  return mockApiGet(
    me ? { '/api/v1/auth/me': { body: meMember }, '/api/v1/family': { body: fam } } : signedOut,
  );
}

/** Renders the probe and waits until `me` (and, signed in, the family) is known. */
async function renderProbe(lesson: ScheduleLesson, now = friday) {
  render(<Toaster />);
  const rendered = renderWithQuery(<Probe lesson={lesson} now={now} />);
  await settled(rendered.queryClient);
  return rendered;
}

async function settled(queryClient: QueryClient) {
  await waitFor(() => expect(queryClient.getQueryState(meQuery.queryKey)?.status).toBe('success'));
  if (queryClient.getQueryData(meQuery.queryKey)) {
    await waitFor(() =>
      expect(queryClient.getQueryState(familyQuery.queryKey)?.status).not.toBe('pending'),
    );
  }
}

const kind = () => screen.getByTestId('kind').textContent;

describe('useLessonQuickAction', () => {
  it('has nothing to do for a guest, and does not ask for a family', async () => {
    const get = mockMember({ me: false });
    await renderProbe(lessonBallet);

    expect(kind()).toBe('none');
    // A guest's request would answer 401, which signs out.
    expect(get).not.toHaveBeenCalledWith('/api/v1/family', expect.anything());
  });

  it('has nothing to do for a canceled, started, ended or closed lesson', async () => {
    mockMember();
    const lessons: [ScheduleLesson, number][] = [
      [lessonContemporary, friday],
      // Started: Monday 18:30 Berlin.
      [lessonBallet, Date.parse('2026-10-26T17:30:00Z')],
      // Ended: Monday 20:00 Berlin.
      [lessonBallet, Date.parse('2026-10-26T19:00:00Z')],
      // Past the booking cutoff: Monday 17:30 Berlin.
      [lessonBallet, Date.parse('2026-10-26T16:30:00Z')],
      // A room rental, which only staff see, takes no bookings.
      [
        {
          ...lessonBallet,
          kind: 'rental',
          visibility: 'hidden',
          instructor: undefined,
          accompanist: undefined,
          renter: 'Copperleaf Theater Group',
          capacity: 0,
          freePlaces: 0,
        },
        friday,
      ],
    ];
    for (const [lesson, now] of lessons) {
      const { unmount } = await renderProbe(lesson, now);
      expect(kind()).toBe('none');
      unmount();
    }
  });

  it('books a lesson with free places for the one person', async () => {
    mockMember();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/bookings': {
        status: 201,
        body: {
          id: bookingId,
          lessonId: ids.lessonBallet,
          personId: meMember.personId,
          status: 'booked',
          source: 'self',
          bookedAt: '2026-10-23T10:00:00Z',
          late: false,
        },
      },
    });
    const user = userEvent.setup();
    await renderProbe(lessonBallet);

    expect(kind()).toBe('book');
    await user.click(screen.getByRole('button', { name: `run for ${meMember.personId}` }));

    expect(await screen.findByText('Nora Example is booked.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/bookings', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { personId: meMember.personId },
    });
  });

  it('says in a toast why the API refused a booking as closed, then has nothing to do', async () => {
    mockMember();
    mockApiPost({
      '/api/v1/lessons/{lessonId}/bookings': conflict('closed', 'closed', { why: 'release' }),
    });
    const user = userEvent.setup();
    await renderProbe(lessonBallet);

    await user.click(screen.getByRole('button', { name: /^run for/ }));

    expect(await screen.findByText('This lesson cannot be booked yet.')).toBeInTheDocument();
    await waitFor(() => expect(kind()).toBe('none'));
  });

  it('cancels a booking through the confirmation dialog', async () => {
    mockMember();
    const post = mockApiPost({
      '/api/v1/bookings/{bookingId}/cancel': {
        body: {
          id: bookingId,
          lessonId: ids.lessonBallet,
          personId: meMember.personId,
          status: 'canceled',
          source: 'self',
          bookedAt: '2026-10-22T10:00:00Z',
          late: false,
        },
      },
    });
    const user = userEvent.setup();
    await renderProbe({ ...lessonBallet, freePlaces: 11, bookedFor: [nora] });

    expect(kind()).toBe('cancel');
    await user.click(screen.getByRole('button', { name: `run for ${meMember.personId}` }));

    const dialog = await screen.findByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(dialog).toHaveAccessibleDescription(
      'Nora Example will no longer be booked for Ballet, Monday, October 26, 18:00–19:30.',
    );
    expect(
      within(dialog).queryByText('This counts as a late cancellation.'),
    ).not.toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole('button', { name: 'Cancel booking' }));
    expect(
      await screen.findByText('The booking of Nora Example has been canceled.'),
    ).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/bookings/{bookingId}/cancel', {
      params: { path: { bookingId } },
    });
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
  });

  it('warns in the dialog once canceling is late', async () => {
    mockMember();
    const user = userEvent.setup();
    // Monday 12:00 Berlin: after the cancellation cutoff, before the booking cutoff.
    await renderProbe(
      { ...lessonBallet, freePlaces: 11, bookedFor: [nora] },
      Date.parse('2026-10-26T10:00:00Z'),
    );

    await user.click(screen.getByRole('button', { name: /^run for/ }));

    const dialog = await screen.findByRole('alertdialog', { name: 'Cancel this booking?' });
    expect(within(dialog).getByText('This counts as a late cancellation.')).toBeInTheDocument();
  });

  it('still offers to cancel after the booking cutoff', async () => {
    mockMember();
    await renderProbe(
      { ...lessonBallet, freePlaces: 11, bookedFor: [nora] },
      Date.parse('2026-10-26T16:30:00Z'),
    );

    expect(kind()).toBe('cancel');
  });

  it('waits with the cancel while the booking is not saved yet', async () => {
    mockMember();
    await renderProbe({ ...lessonBallet, bookedFor: [{ ...nora, bookingId: '' }] });

    expect(kind()).toBe('cancel');
    expect(screen.getByRole('button', { name: /^run for/ })).toBeDisabled();
  });

  it('joins the waiting list of a full lesson, without staying on the list', async () => {
    mockMember();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/waitlist': {
        status: 201,
        body: {
          id: '0190a000-0000-7000-8000-000000000701',
          lessonId: ids.lessonBallet,
          personId: meMember.personId,
          status: 'waiting',
          position: 2,
          stayOnList: false,
          joinedAt: '2026-10-23T10:00:00Z',
        },
      },
    });
    const user = userEvent.setup();
    await renderProbe({ ...lessonBallet, freePlaces: 0 });

    expect(kind()).toBe('waitlist');
    await user.click(screen.getByRole('button', { name: `run for ${meMember.personId}` }));

    expect(
      await screen.findByText('Nora Example is on the waiting list, position 2.'),
    ).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/waitlist', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { personId: meMember.personId, stayOnList: false },
    });
  });

  it('opens the sheet for someone on the waiting list', async () => {
    mockMember();
    const entry: WaitlistFor = {
      entryId: '0190a000-0000-7000-8000-000000000701',
      personId: meMember.personId,
      name: 'Nora Example',
      status: 'waiting',
      position: 1,
      stayOnList: false,
    };
    await renderProbe({ ...lessonBallet, freePlaces: 0, waitlist: [entry] });

    expect(kind()).toBe('open');
  });

  it('opens the sheet when the account books for more than one person', async () => {
    const post = mockApiPost({});
    mockMember({ fam: { ...family, dependents: [family.dependents[0]!] } });
    await renderProbe(lessonBallet);

    expect(kind()).toBe('open');
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('opens the sheet while the family is still loading', async () => {
    // The family's answer is held back; everything else as for Nora alone.
    const get = mockMember();
    const answer = get.getMockImplementation() as (path: string, init?: MockRequest) => unknown;
    get.mockImplementation(((path: string, init?: MockRequest) =>
      path === '/api/v1/family'
        ? new Promise(() => undefined)
        : answer(path, init)) as unknown as typeof api.GET);
    render(<Toaster />);
    const { queryClient } = renderWithQuery(<Probe lesson={lessonBallet} now={friday} />);

    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/family', expect.anything()));
    expect(queryClient.getQueryState(familyQuery.queryKey)?.status).toBe('pending');
    expect(kind()).toBe('open');
  });

  it('opens the sheet when the family cannot be loaded', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meMember } });
    const { queryClient } = await renderProbe(lessonBallet);

    expect(queryClient.getQueryState(familyQuery.queryKey)?.status).toBe('error');
    expect(kind()).toBe('open');
  });
});
