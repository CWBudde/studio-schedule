import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import {
  catalogFixtures,
  eventDraft,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { EventsPage } from './EventsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

it('lists events and lets the office correct a server-refused slug when creating one', async () => {
  const added = {
    ...eventDraft,
    id: 'second-event',
    title: 'Winter Dance Lab',
    slug: 'winter-lab',
  };
  let created = false;
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/admin/events': () => ({
      body: { items: created ? [eventDraft, added] : [eventDraft] },
    }),
  });
  const post = mockApiPost({
    '/api/v1/admin/events': ({ body }) => {
      if ((body as { slug?: string }).slug !== 'winter-lab') {
        return problem(422, 'slug was refused', [{ location: 'body.slug' }]);
      }
      created = true;
      return { status: 201, body: added };
    },
  });
  const user = userEvent.setup();
  renderInRouter(<EventsPage />, { at: '/admin/events' });

  expect(await screen.findByRole('link', { name: eventDraft.title })).toHaveAttribute(
    'href',
    `/admin/events/${eventDraft.id}`,
  );
  await user.click(screen.getByRole('button', { name: 'Add event' }));
  const sheet = within(await screen.findByRole('dialog', { name: 'Add event' }));
  await user.type(await sheet.findByLabelText('Title'), 'Winter Dance Lab');
  await user.click(sheet.getByLabelText('Course type'));
  await user.click(await screen.findByRole('option', { name: 'Ballet' }));
  await user.type(sheet.getByLabelText('Capacity'), '12');
  await user.click(sheet.getByRole('button', { name: 'Add event' }));
  expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
  expect(sheet.getByLabelText('Slug')).toHaveAttribute('aria-invalid', 'true');

  await user.type(sheet.getByLabelText('Slug'), 'winter-lab');
  await user.click(sheet.getByRole('button', { name: 'Add event' }));

  expect(await screen.findByRole('link', { name: 'Winter Dance Lab' })).toBeInTheDocument();
  expect(post).toHaveBeenLastCalledWith('/api/v1/admin/events', {
    body: {
      title: 'Winter Dance Lab',
      slug: 'winter-lab',
      courseTypeId: eventDraft.courseTypeId,
      capacity: 12,
      price: 0,
      vatRate: 0,
    },
  });
});
