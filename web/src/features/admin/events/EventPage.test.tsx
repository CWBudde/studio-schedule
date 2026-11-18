import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import {
  catalogFixtures,
  eventDraft,
  eventPublished,
  eventSession,
  guestRegistration,
  conflict,
  ids,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
  publicSettings,
  receiptSettings,
  staffEntries,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { EventPage } from './EventPage';

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const eventPath = '/api/v1/admin/events/{eventId}';
const registrationsPath = '/api/v1/admin/events/{eventId}/registrations';

function renderPage() {
  return renderInRouter(<EventPage eventId={eventDraft.id} />, { at: '/admin/events/$eventId' });
}

it('shows the reason why an event without sessions cannot be published', async () => {
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    [eventPath]: { body: eventDraft },
    [registrationsPath]: { body: { items: [] } },
  });
  mockApiPost({ '/api/v1/admin/events/{eventId}/publish': conflict('no-sessions') });
  const user = userEvent.setup();
  renderPage();

  await user.click(await screen.findByRole('button', { name: 'Publish' }));
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Add a scheduled session before publishing.',
  );
  expect(screen.getByText('Draft')).toBeInTheDocument();
});

it('requires explicit confirmation before registering a guest beyond capacity', async () => {
  let added = false;
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    [eventPath]: { body: { ...eventPublished, registered: eventPublished.capacity } },
    [registrationsPath]: () => ({ body: { items: added ? [guestRegistration] : [] } }),
  });
  const post = mockApiPost({
    [registrationsPath]: ({ body }) => {
      if (!(body as { confirmOverCapacity?: boolean }).confirmOverCapacity) return conflict('full');
      added = true;
      return { status: 201, body: guestRegistration };
    },
  });
  const user = userEvent.setup();
  renderPage();
  await user.click(await screen.findByRole('button', { name: 'Add registration' }));
  const dialog = within(await screen.findByRole('dialog'));
  await user.click(dialog.getByRole('tab', { name: 'Guest' }));
  await user.type(dialog.getByLabelText('Guest name'), 'Taylor Example');
  await user.type(dialog.getByLabelText('Guest email'), 'taylor@example.org');
  await user.click(dialog.getByRole('button', { name: 'Add registration' }));

  expect(await dialog.findByRole('alert')).toHaveTextContent('The event is full.');
  expect(post).toHaveBeenCalledTimes(1);
  await user.click(dialog.getByRole('button', { name: 'Register anyway' }));

  expect(await screen.findByRole('cell', { name: 'Taylor Example' })).toBeInTheDocument();
  expect(post).toHaveBeenLastCalledWith(registrationsPath, {
    params: { path: { eventId: eventDraft.id } },
    body: {
      guest: { name: 'Taylor Example', email: 'taylor@example.org' },
      confirmOverCapacity: true,
    },
  });
});

const paid = { ...guestRegistration, payment: 'paid', receiptId: 'event-receipt' } as const;

it('records a desk payment and offers to download the resulting receipt', async () => {
  let registration = guestRegistration;
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    [eventPath]: { body: eventPublished },
    [registrationsPath]: () => ({ body: { items: [registration] } }),
    '/api/v1/admin/settings/receipts': { body: receiptSettings },
  });
  const post = mockApiPost({
    '/api/v1/admin/event-registrations/{registrationId}/pay': () => {
      registration = paid;
      return {
        body: {
          registration,
          receipt: { id: paid.receiptId, issueDate: '2026-10-01', number: 'EX-2026-0001' },
        },
      };
    },
  });
  const open = vi.spyOn(window, 'open').mockReturnValue(null);
  const user = userEvent.setup();
  renderPage();

  await user.click(await screen.findByRole('button', { name: 'Mark paid' }));
  const dialog = within(await screen.findByRole('dialog'));
  await user.click(dialog.getByRole('button', { name: 'Mark paid' }));

  await user.click(await screen.findByRole('button', { name: 'Download receipt' }));
  expect(open).toHaveBeenCalledWith(
    '/api/v1/admin/receipts/event-receipt/pdf',
    '_blank',
    'noopener',
  );
  expect(post).toHaveBeenCalledWith('/api/v1/admin/event-registrations/{registrationId}/pay', {
    params: { path: { registrationId: guestRegistration.id } },
    body: {
      paymentMethod: 'cash',
      recipientName: 'Taylor Example',
      recipientEmail: 'taylor@example.org',
    },
  });
});

it('cancels a paid registration with a refund when the office chooses it', async () => {
  let registration: typeof guestRegistration = paid;
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    [eventPath]: { body: eventPublished },
    [registrationsPath]: () => ({ body: { items: [registration] } }),
  });
  const post = mockApiPost({
    '/api/v1/admin/event-registrations/{registrationId}/cancel': () => {
      registration = {
        ...paid,
        status: 'canceled',
        payment: 'refunded',
        cancellationReceiptId: 'storno',
        canceledAt: '2026-10-01T10:00:00Z',
      };
      return { body: registration };
    },
  });
  const user = userEvent.setup();
  renderPage();

  await user.click(await screen.findByRole('button', { name: 'Cancel registration' }));
  const dialog = within(await screen.findByRole('alertdialog'));
  await user.click(dialog.getByRole('checkbox', { name: 'Refund the fee' }));
  await user.click(dialog.getByRole('button', { name: 'Cancel registration' }));

  expect(await screen.findByRole('cell', { name: 'Refunded' })).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Download cancellation receipt' })).toBeInTheDocument();
  expect(post).toHaveBeenCalledWith('/api/v1/admin/event-registrations/{registrationId}/cancel', {
    params: { path: { registrationId: guestRegistration.id } },
    body: { refund: true },
  });
});

it('adds a session using lesson fields and updates the event’s session list', async () => {
  const session = eventSession;
  let sessions: typeof eventDraft.sessions = [];
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/admin/staff': { body: { items: staffEntries } },
    [eventPath]: () => ({ body: { ...eventDraft, sessions } }),
    [registrationsPath]: { body: { items: [] } },
  });
  const post = mockApiPost({
    '/api/v1/admin/events/{eventId}/sessions': () => {
      sessions = [session];
      return { status: 201, body: session };
    },
  });
  const user = userEvent.setup();
  renderPage();

  await user.click(await screen.findByRole('button', { name: 'Add session' }));
  const sheet = within(await screen.findByRole('dialog', { name: 'Add session' }));
  fireEvent.change(await sheet.findByLabelText('Date'), { target: { value: '2026-11-07' } });
  fireEvent.change(sheet.getByLabelText('Start time'), { target: { value: '14:00' } });
  await user.type(sheet.getByLabelText('Duration (minutes)'), '180');
  await user.click(sheet.getByRole('combobox', { name: 'Room' }));
  await user.click(await screen.findByRole('option', { name: 'North Studio · Room A' }));
  await user.click(sheet.getByRole('combobox', { name: 'Instructor' }));
  await user.click(await screen.findByRole('option', { name: 'Riley Sample' }));
  await user.click(sheet.getByRole('button', { name: 'Add session' }));

  expect(await screen.findByRole('button', { name: /Manage session on/ })).toBeInTheDocument();
  expect(post).toHaveBeenCalledWith('/api/v1/admin/events/{eventId}/sessions', {
    params: { path: { eventId: eventDraft.id } },
    body: {
      date: '2026-11-07',
      startTime: '14:00',
      durationMinutes: 180,
      roomId: ids.roomA,
      instructorId: ids.staffRiley,
    },
  });
});

it('keeps session capacity and visibility fixed and reports their validation errors', async () => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-23T10:00:00Z'));
  const session = { ...eventSession, capacity: 1200, freePlaces: 1200 };
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/admin/staff': { body: { items: staffEntries } },
    '/api/v1/lessons/{lessonId}': { body: session },
    [eventPath]: { body: { ...eventDraft, capacity: 1200, sessions: [session] } },
    [registrationsPath]: { body: { items: [] } },
  });
  const put = mockApiPut({
    '/api/v1/lessons/{lessonId}': problem(422, 'session fields cannot change', [
      { location: 'body.capacity' },
      { location: 'body.visibility' },
    ]),
  });
  const user = userEvent.setup();
  renderPage();

  await user.click(await screen.findByRole('button', { name: /Manage session on/ }));
  await user.click(await screen.findByRole('button', { name: 'Edit' }));
  const sheet = within(await screen.findByRole('dialog', { name: 'Edit session' }));
  const capacity = await sheet.findByLabelText('Capacity');
  const visibility = sheet.getByLabelText('Visibility');
  expect(capacity).toHaveAttribute('readonly');
  expect(visibility).toHaveAttribute('readonly');
  fireEvent.change(sheet.getByLabelText('Start time'), { target: { value: '15:00' } });
  await user.click(sheet.getByRole('button', { name: 'Save changes' }));

  expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
  await waitFor(() => {
    expect(capacity).toHaveAttribute('aria-invalid', 'true');
    expect(visibility).toHaveAttribute('aria-invalid', 'true');
  });
  expect(put).toHaveBeenCalledWith('/api/v1/lessons/{lessonId}', {
    params: { path: { lessonId: session.id } },
    body: {
      kind: 'workshop',
      date: '2026-11-07',
      startTime: '15:00',
      durationMinutes: 180,
      roomId: ids.roomA,
      instructorId: ids.staffRiley,
      capacity: 1200,
      visibility: 'public',
    },
  });
});
