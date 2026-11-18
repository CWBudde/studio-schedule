import { screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { EventView } from '@/themes/slots';
import { renderInRouter } from '@/test/utils';
import { EventDetail } from './EventDetail';
import { EventList } from './EventList';

const session = {
  id: 'session',
  date: 'Saturday, November 7',
  time: '14:00–17:00',
  place: 'Room A · North Studio',
  teachers: 'Ada Example',
  canceled: false,
};
const event: EventView = {
  title: 'Example Intensive',
  courseType: 'Contemporary',
  slug: 'example-intensive',
  price: '€45.00',
  availability: '1 place left',
  sessions: [session],
  nextSession: session,
};

describe('Werkstatt event slots', () => {
  it('renders dates, fee and places left in the event link', async () => {
    renderInRouter(<EventList events={[event]} />, { at: '/events' });
    const link = await screen.findByRole('link', { name: /Example Intensive/ });
    expect(link).toHaveAttribute('href', '/events/example-intensive');
    expect(link).toHaveTextContent(session.time);
    expect(link).toHaveTextContent(event.price);
    expect(link).toHaveTextContent(event.availability);
  });

  it('renders each session and the connected registration controls', async () => {
    const canceled = { ...session, id: 'other', canceled: true };
    renderInRouter(
      <EventDetail
        event={{ ...event, sessions: [session, canceled] }}
        registration={<button>Register</button>}
      />,
      { at: '/events' },
    );
    const article = await screen.findByRole('article');
    expect(within(article).getByRole('heading', { name: event.title })).toBeVisible();
    const rows = within(article).getAllByRole('listitem');
    expect(rows[0]).toHaveTextContent(session.teachers);
    expect(rows[1]).toHaveTextContent('Event canceled');
    expect(within(article).getByRole('button', { name: 'Register' })).toBeVisible();
  });
});
