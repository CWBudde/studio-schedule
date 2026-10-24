import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { LessonDetail } from '@/api/client';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  catalogFixtures,
  conflict,
  ids,
  lessonBalletOffice,
  lessonContemporaryOffice,
  lessonWorkshopOffice,
  meMember,
  meOffice,
  type MockRequest,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
  publicSettings,
  staffEntries,
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

const officeLessons: Record<string, LessonDetail> = {
  [ids.lessonBallet]: lessonBalletOffice,
  [ids.lessonWorkshop]: lessonWorkshopOffice,
  [ids.lessonContemporary]: lessonContemporaryOffice,
};

/** The API as the office (or `me`) sees it, with `lessons` as the lesson details. */
function mockOffice({ me = meOffice, lessons = officeLessons } = {}) {
  return mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: me },
    '/api/v1/admin/staff': { body: { items: staffEntries } },
    '/api/v1/lessons/{lessonId}': (init: MockRequest) => {
      const lesson = lessons[init.params?.path?.lessonId ?? ''];
      return lesson ? { body: lesson } : { status: 404, body: { title: 'Not Found' } };
    },
  });
}

/** Opens the lesson sheet of `lessonId` and waits for its office section. */
async function openAsOffice(lessonId: string, name: string) {
  render(<Toaster />);
  const rendered = renderAtSchedule(<LessonSheet />, { lesson: lessonId });
  const dialog = await screen.findByRole('dialog', { name });
  const office = await within(dialog).findByRole('region', { name: 'Manage this lesson' });
  return { ...rendered, dialog, office };
}

/** The request body that keeps the ballet lesson as it is, as an edit sends it. */
const balletBody = {
  date: '2026-10-26',
  startTime: '18:00',
  durationMinutes: 90,
  roomId: ids.roomA,
  instructorId: ids.staffAlex,
  accompanistId: ids.staffSam,
  capacity: 12,
  visibility: 'public',
  kind: 'regular',
};

describe('LessonSheet for the office', () => {
  it('offers the office actions with the bookings of a lesson of a class', async () => {
    mockOffice();

    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');

    expect(within(office).getByText('2 active bookings')).toBeInTheDocument();
    expect(within(office).getByRole('button', { name: 'Edit' })).toBeEnabled();
    expect(within(office).getByRole('button', { name: 'Hide' })).toBeEnabled();
    expect(within(office).getByRole('button', { name: 'Cancel lesson' })).toBeEnabled();
    // A lesson of a class is canceled, never deleted.
    expect(within(office).queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument();
    expect(within(office).queryByText(/^Edited individually/)).not.toBeInTheDocument();
  });

  it('offers nothing to a member', async () => {
    mockOffice({ me: meMember, lessons: { [ids.lessonBallet]: lessonBalletOffice } });

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonBallet });

    const dialog = await screen.findByRole('dialog', { name: 'Ballet' });
    expect(await within(dialog).findByText('Room A')).toBeInTheDocument();
    expect(within(dialog).queryByRole('region', { name: 'Manage this lesson' })).toBeNull();
    expect(within(dialog).queryByRole('button', { name: 'Edit' })).toBeNull();
  });

  it('disables the actions once the lesson has started', async () => {
    vi.setSystemTime(new Date('2026-10-26T17:15:00Z'));
    mockOffice();

    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');

    expect(
      within(office).getByText('This lesson has started and can no longer be changed.'),
    ).toBeInTheDocument();
    for (const name of ['Edit', 'Hide', 'Cancel lesson']) {
      expect(within(office).getByRole('button', { name })).toBeDisabled();
    }
  });

  it('hides a booked lesson after a confirmation with the number of bookings', async () => {
    mockOffice();
    const put = mockApiPut({
      '/api/v1/lessons/{lessonId}': { body: { ...lessonBalletOffice, visibility: 'hidden' } },
    });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');

    await user.click(within(office).getByRole('button', { name: 'Hide' }));

    const confirm = await screen.findByRole('alertdialog', { name: 'Hide this lesson?' });
    expect(confirm).toHaveTextContent(
      'It has 2 active bookings. Once hidden, only staff see it; nobody gets a mail.',
    );
    expect(put).not.toHaveBeenCalled();
    await user.click(within(confirm).getByRole('button', { name: 'Hide lesson' }));

    await waitFor(() => expect(confirm).not.toBeInTheDocument());
    expect(put).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { ...balletBody, visibility: 'hidden' },
    });
    expect(await screen.findByText('The lesson is hidden: only staff see it.')).toBeInTheDocument();
  });

  it('hides an unbooked lesson at once and makes a hidden one public again', async () => {
    // The API keeps the visibility it is sent.
    const lessons = { [ids.lessonWorkshop]: lessonWorkshopOffice };
    mockOffice({ lessons });
    const put = mockApiPut({
      '/api/v1/lessons/{lessonId}': ({ body }) => {
        const { visibility } = body as { visibility: LessonDetail['visibility'] };
        lessons[ids.lessonWorkshop] = { ...lessonWorkshopOffice, visibility };
        return { body: lessons[ids.lessonWorkshop] };
      },
    });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonWorkshop, 'Jazz');

    await user.click(within(office).getByRole('button', { name: 'Hide' }));

    await waitFor(() => expect(put).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(put.mock.calls[0]?.[1]).toEqual({
      params: { path: { lessonId: ids.lessonWorkshop } },
      body: {
        date: '2026-11-07',
        startTime: '14:00',
        durationMinutes: 180,
        roomId: ids.roomA,
        instructorId: ids.staffRiley,
        capacity: 20,
        visibility: 'hidden',
        kind: 'workshop',
      },
    });

    await user.click(await within(office).findByRole('button', { name: 'Unhide' }));

    await waitFor(() => expect(put).toHaveBeenCalledTimes(2));
    expect(put.mock.calls[1]?.[1]).toMatchObject({ body: { visibility: 'public' } });
    expect(await screen.findByText('The lesson is public again.')).toBeInTheDocument();
  });

  it('cancels a lesson with a note and says who gets a mail', async () => {
    mockOffice();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/cancel': {
        body: { ...lessonBalletOffice, status: 'canceled' },
      },
    });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');

    await user.click(within(office).getByRole('button', { name: 'Cancel lesson' }));

    const confirm = await screen.findByRole('alertdialog', { name: 'Cancel this lesson?' });
    expect(confirm).toHaveTextContent(
      'It has 2 active bookings. Everyone booked gets a mail about the cancellation, with the note.',
    );
    await user.type(within(confirm).getByLabelText('Note (optional)'), 'Room flooded.');
    await user.click(within(confirm).getByRole('button', { name: 'Cancel lesson' }));

    await waitFor(() => expect(confirm).not.toBeInTheDocument());
    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/cancel', {
      params: { path: { lessonId: ids.lessonBallet } },
      body: { note: 'Room flooded.' },
    });
    expect(await screen.findByText('The lesson has been canceled.')).toBeInTheDocument();
  });

  it('cancels an unbooked lesson without a note', async () => {
    mockOffice();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/cancel': { body: lessonWorkshopOffice },
    });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonWorkshop, 'Jazz');

    await user.click(within(office).getByRole('button', { name: 'Cancel lesson' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Cancel this lesson?' });
    expect(confirm).toHaveTextContent('Nobody has booked it, so nobody gets a mail.');
    await user.click(within(confirm).getByRole('button', { name: 'Cancel lesson' }));

    await waitFor(() => expect(post).toHaveBeenCalled());
    expect(post.mock.calls[0]?.[1]).toEqual({
      params: { path: { lessonId: ids.lessonWorkshop } },
      body: {},
    });
  });

  it('counts the characters of a note as the API does, not UTF-16 units', async () => {
    mockOffice();
    const post = mockApiPost({ '/api/v1/lessons/{lessonId}/cancel': { body: lessonBalletOffice } });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');
    await user.click(within(office).getByRole('button', { name: 'Cancel lesson' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Cancel this lesson?' });
    const note = within(confirm).getByLabelText('Note (optional)');

    // 501 characters, but 500 once trimmed; each emoji is two UTF-16 units.
    await user.click(note);
    await user.paste(`${'💃'.repeat(500)} `);
    await user.click(within(confirm).getByRole('button', { name: 'Cancel lesson' }));
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    expect(post.mock.calls[0]?.[1]).toMatchObject({ body: { note: '💃'.repeat(500) } });
  });

  it('refuses a note of 501 characters', async () => {
    mockOffice();
    const post = mockApiPost({});
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');
    await user.click(within(office).getByRole('button', { name: 'Cancel lesson' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Cancel this lesson?' });

    await user.click(within(confirm).getByLabelText('Note (optional)'));
    await user.paste('💃'.repeat(501));
    await user.click(within(confirm).getByRole('button', { name: 'Cancel lesson' }));

    expect(await within(confirm).findByText('This entry is too long.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('shows another 422 without a field as a general failure', async () => {
    mockOffice();
    mockApiPost({ '/api/v1/lessons/{lessonId}/restore': problem(422, 'something else') });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonContemporary, 'Contemporary');

    await user.click(within(office).getByRole('button', { name: 'Restore' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Restore this lesson?' });
    await user.click(within(confirm).getByRole('button', { name: 'Restore lesson' }));

    expect(await within(confirm).findByRole('alert')).toHaveTextContent(
      'Something went wrong. Please try again.',
    );
  });

  it('refuses a note that is too long, and marks the note the API refuses', async () => {
    mockOffice();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/cancel': problem(422, 'validation failed', [
        { location: 'body.note', message: 'too long' },
      ]),
    });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');
    await user.click(within(office).getByRole('button', { name: 'Cancel lesson' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Cancel this lesson?' });
    const note = within(confirm).getByLabelText('Note (optional)');

    await user.click(note);
    await user.paste('x'.repeat(501));
    await user.click(within(confirm).getByRole('button', { name: 'Cancel lesson' }));

    expect(await within(confirm).findByText('This entry is too long.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();

    await user.clear(note);
    await user.type(note, 'Fine.');
    await user.click(within(confirm).getByRole('button', { name: 'Cancel lesson' }));

    await waitFor(() => expect(note).toHaveAttribute('aria-invalid', 'true'));
    expect(post).toHaveBeenCalledTimes(1);
    expect(confirm).toBeInTheDocument();
  });

  it('says so when the lesson has started meanwhile (422 without a field)', async () => {
    mockOffice();
    mockApiPost({
      '/api/v1/lessons/{lessonId}/cancel': problem(422, 'the lesson has started', [
        { location: 'path.lessonId', message: 'the lesson has started' },
      ]),
    });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonBallet, 'Ballet');

    await user.click(within(office).getByRole('button', { name: 'Cancel lesson' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Cancel this lesson?' });
    await user.click(within(confirm).getByRole('button', { name: 'Cancel lesson' }));

    expect(await within(confirm).findByRole('alert')).toHaveTextContent(
      'This lesson can no longer be changed; it may have started meanwhile.',
    );
  });

  it('shows who canceled when, the note and the detached hint, and restores', async () => {
    mockOffice();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/restore': {
        body: { ...lessonContemporaryOffice, status: 'scheduled' },
      },
    });
    const user = userEvent.setup();
    const { dialog, office } = await openAsOffice(ids.lessonContemporary, 'Contemporary');

    expect(
      within(dialog).getByText('Canceled by Otto Office on Oct 23, 2026, 12:30.'),
    ).toBeInTheDocument();
    expect(within(dialog).getByText('Note: The instructor is ill.')).toBeInTheDocument();
    expect(
      within(office).getByText(
        'Edited individually: changes to its class no longer reach this lesson.',
      ),
    ).toBeInTheDocument();
    expect(within(office).queryByRole('button', { name: 'Cancel lesson' })).toBeNull();

    await user.click(within(office).getByRole('button', { name: 'Restore' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Restore this lesson?' });
    expect(confirm).toHaveTextContent(
      'It takes place after all. It has 1 active booking; everyone booked gets a mail.',
    );
    await user.click(within(confirm).getByRole('button', { name: 'Restore lesson' }));

    await waitFor(() => expect(confirm).not.toBeInTheDocument());
    expect(post).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}/restore', {
      params: { path: { lessonId: ids.lessonContemporary } },
      body: {},
    });
    // Sonner keeps the toasts of earlier tests in this file.
    expect(await screen.findAllByText('The lesson takes place again.')).not.toHaveLength(0);
  });

  it('names the lesson that took the room meanwhile and restores anyway on request', async () => {
    mockOffice();
    const post = mockApiPost({
      '/api/v1/lessons/{lessonId}/restore': ({ body }) =>
        (body as { confirmRoomOverlap?: boolean }).confirmRoomOverlap
          ? { body: { ...lessonContemporaryOffice, status: 'scheduled' } }
          : conflict('room-taken', 'room taken', {
              lesson: {
                id: ids.lessonWorkshop,
                courseType: { id: ids.jazz, name: 'Jazz' },
                startsAt: '2026-10-27T15:30:00Z',
                endsAt: '2026-10-27T16:30:00Z',
              },
            }),
    });
    const user = userEvent.setup();
    const { office } = await openAsOffice(ids.lessonContemporary, 'Contemporary');

    await user.click(within(office).getByRole('button', { name: 'Restore' }));
    const confirm = await screen.findByRole('alertdialog', { name: 'Restore this lesson?' });
    await user.click(within(confirm).getByRole('button', { name: 'Restore lesson' }));

    const alert = await within(confirm).findByRole('alert');
    expect(alert).toHaveTextContent('Room B is taken at that time');
    expect(alert).toHaveTextContent('Jazz, Tuesday, October 27, 16:30–17:30 uses the room.');
    await user.click(within(alert).getByRole('button', { name: 'Restore anyway' }));

    await waitFor(() => expect(confirm).not.toBeInTheDocument());
    expect(post.mock.calls.map((call) => (call[1] as { body: object }).body)).toEqual([
      {},
      { confirmRoomOverlap: true },
    ]);
    // Sonner keeps the toasts of earlier tests in this file.
    expect(await screen.findAllByText('The lesson takes place again.')).not.toHaveLength(0);
  });

  it('deletes a one-off lesson and closes the sheet', async () => {
    mockOffice();
    const remove = mockApiDelete({ '/api/v1/lessons/{lessonId}': { status: 204 } });
    const user = userEvent.setup();
    const { office, router } = await openAsOffice(ids.lessonWorkshop, 'Jazz');

    await user.click(within(office).getByRole('button', { name: 'Delete' }));
    const confirm = await screen.findByRole('alertdialog', {
      name: 'Delete this one-off lesson?',
    });
    expect(confirm).toHaveTextContent('Jazz, Saturday, November 7, 14:00–17:00');
    await user.click(within(confirm).getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(router.state.location.search).toEqual({}));
    expect(remove).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', {
      params: { path: { lessonId: ids.lessonWorkshop } },
    });
    expect(await screen.findByText('The lesson has been deleted.')).toBeInTheDocument();
  });

  it('says why a booked one-off lesson cannot be deleted (409 in-use)', async () => {
    mockOffice();
    mockApiDelete({ '/api/v1/lessons/{lessonId}': conflict('in-use') });
    const user = userEvent.setup();
    const { office, router } = await openAsOffice(ids.lessonWorkshop, 'Jazz');

    await user.click(within(office).getByRole('button', { name: 'Delete' }));
    const confirm = await screen.findByRole('alertdialog', {
      name: 'Delete this one-off lesson?',
    });
    await user.click(within(confirm).getByRole('button', { name: 'Delete' }));

    expect(await within(confirm).findByRole('alert')).toHaveTextContent(
      'Someone has booked this lesson, so it cannot be deleted. Cancel it instead.',
    );
    expect(router.state.location.search).toEqual({ lesson: ids.lessonWorkshop });
  });

  it('renders in German', async () => {
    mockOffice();
    await i18n.changeLanguage('de');

    renderAtSchedule(<LessonSheet />, { lesson: ids.lessonContemporary });
    const sheet = await screen.findByRole('dialog', { name: 'Contemporary' });
    const office = await within(sheet).findByRole('region', { name: 'Stunde verwalten' });
    expect(
      within(sheet).getByText('Abgesagt von Otto Office am 23. Okt. 2026, 12:30.'),
    ).toBeInTheDocument();
    expect(within(office).getByRole('button', { name: 'Wieder ansetzen' })).toBeInTheDocument();
    expect(within(office).getByText('1 aktive Buchung')).toBeInTheDocument();
  });
});
