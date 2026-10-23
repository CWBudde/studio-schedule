import { createFileRoute } from '@tanstack/react-router';
import { z } from 'zod';
import { ResetPasswordPage } from '@/features/auth/ResetPasswordPage';

// The link in the reset mail: /reset-password?token=… (an invalid token is dropped).
const resetSearchSchema = z.object({
  token: z.string().min(1).max(100).optional().catch(undefined),
});

export const Route = createFileRoute('/reset-password')({
  validateSearch: (search: Record<string, unknown>) => resetSearchSchema.parse(search),
  component: ResetPasswordPage,
});
