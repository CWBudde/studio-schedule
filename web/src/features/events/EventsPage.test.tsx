import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { components } from '@/api/schema';
import {
  conflict,
  family,
  lessonWorkshop,
  meMember,
  mockApiGet,
  mockApiPost,
  publicSettings,
  problem,
} from '@/test/mockApi';
import { MyEventsSection } from '@/features/my-lessons/MyEventsSection';
import { renderInRouter } from '@/test/utils';
import { EventPage } from './EventPage';
import { EventsPage } from './EventsPage';
import { EventInviteRoute, InvitePage } from './InvitePage';

const event: components['schemas']['Event'] = {
  slug: 'example-intensive',
  title: 'Example Intensive',
  courseType: { id: 'course-type', name: 'Contemporary' },
  status: 'published',
  capacity: 3,
  freePlaces: 1,
  price: 4500,
  vatRate: 1900,
  signupOpen: true,
  signupClosesAt: '2026-11-07T13:00:00Z',
  cancelUntil: '2026-11-06T22:00:00Z',
  hasImage: false,
  sessions: [lessonWorkshop],
};

describe('public events', () => {
  it('shows when there are no upcoming events', async () => {
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/events': { body: { items: [] } },
    });
    renderInRouter(<EventsPage />, { at: '/events' });

    expect(await screen.findByText('There are no upcoming events.')).toBeVisible();
  });

  it('lists upcoming events with price, dates and availability', async () => {
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/events': {
        body: {
          items: [
            event,
            { ...event, title: 'Example Taster', slug: 'example-taster', price: 0, freePlaces: 0 },
          ],
        },
      },
    });
    renderInRouter(<EventsPage />, { at: '/events' });

    expect(await screen.findByRole('link', { name: /Example Intensive/ })).toHaveAttribute(
      'href',
      '/events/example-intensive',
    );
    expect(screen.getByText('€45.00')).toBeVisible();
    expect(screen.getByText('Free')).toBeVisible();
    expect(screen.getByText('Full')).toBeVisible();
    expect(screen.getAllByText(/14:00/)).toHaveLength(2);
  });

  it('offers the waiting list to the selected dependent when registration fills', async () => {
    let current = event;
    let mine: components['schemas']['MyEvent'][] = [];
    const [dependent] = family.dependents;
    if (!dependent) throw new Error('The test family needs a dependent');
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/events/{slug}': () => ({ body: current }),
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/my/events': () => ({ body: { items: mine } }),
      '/api/v1/family': { body: family },
    });
    const post = mockApiPost({
      '/api/v1/events/{slug}/registrations': () => {
        current = { ...event, freePlaces: 0 };
        return conflict('full');
      },
      '/api/v1/events/{slug}/waitlist': () => {
        const registration: components['schemas']['MyEventRegistration'] = {
          id: 'waiting',
          person: { id: dependent.id, name: dependent.givenName + ' ' + dependent.familyName },
          status: 'waiting',
          payment: 'unpaid',
          price: 4500,
          cancelable: false,
          createdAt: '2026-10-23T16:00:00Z',
        };
        mine = [{ registration, event: current }];
        return { status: 201, body: mine[0] };
      },
    });

    renderInRouter(<EventPage slug={event.slug} />, { at: '/events' });
    const user = userEvent.setup();
    await user.selectOptions(await screen.findByLabelText('Participant'), dependent.id);
    await user.click(screen.getByRole('button', { name: 'Register' }));
    await user.click(await screen.findByRole('button', { name: 'Join waiting list' }));

    expect(await screen.findByText('On the waiting list')).toBeVisible();
    expect(post).toHaveBeenCalledWith('/api/v1/events/{slug}/waitlist', {
      params: { path: { slug: event.slug } },
      body: { personId: dependent.id },
    });
  });
});

describe('event invitation links', () => {
  it('opens an event whose slug is invite when there is no token', async () => {
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/events/{slug}': ({ params }) =>
        params?.path?.slug === 'invite'
          ? { body: { ...event, slug: 'invite' } }
          : problem(404, 'not found'),
    });
    renderInRouter(<EventInviteRoute />, { at: '/events/invite' });
    expect(await screen.findByRole('heading', { name: event.title })).toBeVisible();
  });

  it('loads a reserved place read-only and accepts it while signed out', async () => {
    const token = 'mock-offer';
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/event-invites/{token}': {
        body: { event, givenName: 'Ari', expiresAt: '2026-11-06T21:00:00Z' },
      },
    });
    const post = mockApiPost({ '/api/v1/event-invites/{token}/accept': { status: 204 } });

    renderInRouter(<InvitePage token={token} />, { at: '/events/invite' });
    expect(await screen.findByText(/A place is reserved for Ari until .*22:00/)).toBeVisible();
    expect(post).not.toHaveBeenCalled();

    await userEvent.setup().click(screen.getByRole('button', { name: 'Accept the place' }));
    expect(await screen.findByRole('heading', { name: 'Registration confirmed' })).toBeVisible();
    expect(post).toHaveBeenCalledWith('/api/v1/event-invites/{token}/accept', {
      params: { path: { token } },
    });
  });

  it('reports an expired or used invitation', async () => {
    mockApiGet({ '/api/v1/event-invites/{token}': problem(404, 'not found') });
    renderInRouter(<InvitePage token="used-offer" />, { at: '/events/invite' });
    expect(
      await screen.findByRole('heading', { name: /expired or already been used/ }),
    ).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Accept the place' })).not.toBeInTheDocument();
  });
});

describe('My events', () => {
  const registration: components['schemas']['MyEventRegistration'] = {
    id: 'registration',
    person: { id: 'dependent', name: 'Ari Example' },
    status: 'registered',
    payment: 'paid',
    price: 4500,
    receiptId: 'family-receipt',
    cancelable: true,
    createdAt: '2026-10-23T16:00:00Z',
  };

  function show(eventRegistration = registration) {
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/my/events': { body: { items: [{ registration: eventRegistration, event }] } },
    });
    renderInRouter(<MyEventsSection />, { at: '/my-lessons' });
  }

  it('downloads a dependent’s receipt through the member path', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    show();
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Download receipt' }));
    expect(open).toHaveBeenCalledWith(
      '/api/v1/my/receipts/family-receipt/pdf',
      '_blank',
      'noopener',
    );
  });

  it('shows the price without a payment demand for a canceled registration', async () => {
    show({
      ...registration,
      status: 'canceled',
      payment: 'unpaid',
      receiptId: undefined,
      cancelable: false,
    });
    expect(await screen.findByText(/Ari Example .*€45.00/)).toBeVisible();
    expect(screen.queryByText(/due at the office/)).not.toBeInTheDocument();
  });

  it('shows the office guidance when the cancellation deadline has passed', async () => {
    show({ ...registration, cancelable: false });
    expect(await screen.findByText(/Please contact the office to cancel/)).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Cancel registration' })).not.toBeInTheDocument();
  });
});
