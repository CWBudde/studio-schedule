import { createFileRoute } from '@tanstack/react-router';
import { EventsPage } from '@/features/admin/events/EventsPage';

export const Route = createFileRoute('/admin/events/')({
  component: EventsPage,
});
