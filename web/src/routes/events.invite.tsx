import { createFileRoute } from '@tanstack/react-router';
import { z } from 'zod';
import { EventInviteRoute } from '@/features/events/InvitePage';

const searchSchema = z.object({ token: z.string().min(1).max(128).optional().catch('') });

export const Route = createFileRoute('/events/invite')({
  validateSearch: (search: Record<string, unknown>) => searchSchema.parse(search),
  component: EventInviteRoute,
});
