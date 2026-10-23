import { createFileRoute } from '@tanstack/react-router';
import { z } from 'zod';
import { ConfirmEmailChangePage } from '@/features/auth/ConfirmEmailChangePage';

// The link in the mail to a new address: /confirm-email-change?token=… (an invalid token is dropped).
const confirmSearchSchema = z.object({
  token: z.string().min(1).max(100).optional().catch(undefined),
});

export const Route = createFileRoute('/confirm-email-change')({
  validateSearch: (search: Record<string, unknown>) => confirmSearchSchema.parse(search),
  component: ConfirmEmailChangePage,
});
