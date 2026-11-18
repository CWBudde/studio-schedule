import { createFileRoute } from '@tanstack/react-router';
import { EventRoute } from '@/features/admin/events/EventPage';

export const Route = createFileRoute('/admin/events/$eventId')({
  component: EventRoute,
});
