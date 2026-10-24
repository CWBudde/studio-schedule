import { createFileRoute } from '@tanstack/react-router';
import { z } from 'zod';
import { WaitlistAcceptPage } from '@/features/waitlist/WaitlistAcceptPage';

// The link in a waiting-list invite: /waitlist/accept?token=… (an invalid token is dropped).
const acceptSearchSchema = z.object({
  token: z.string().min(1).max(100).optional().catch(undefined),
});

export const Route = createFileRoute('/waitlist/accept')({
  validateSearch: (search: Record<string, unknown>) => acceptSearchSchema.parse(search),
  component: WaitlistAcceptPage,
});
