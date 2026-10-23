import { createFileRoute } from '@tanstack/react-router';
import { AccountsPage } from '@/features/admin/AccountsPage';

export const Route = createFileRoute('/admin/accounts')({
  component: AccountsPage,
});
