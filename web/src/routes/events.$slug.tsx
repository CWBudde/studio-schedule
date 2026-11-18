import { createFileRoute } from '@tanstack/react-router';
import { PublicEventRoute } from '@/features/events/EventPage';

export const Route = createFileRoute('/events/$slug')({ component: PublicEventRoute });
