import { createFileRoute } from '@tanstack/react-router';
import { z } from 'zod';
import { ConfirmPage } from '@/features/auth/ConfirmPage';

// The link in the confirmation mail: /confirm?token=… (an invalid token is dropped).
const confirmSearchSchema = z.object({
  token: z.string().min(1).max(100).optional().catch(undefined),
});

export const Route = createFileRoute('/confirm')({
  validateSearch: (search: Record<string, unknown>) => confirmSearchSchema.parse(search),
  component: ConfirmPage,
});
