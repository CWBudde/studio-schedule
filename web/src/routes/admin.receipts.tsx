import { createFileRoute } from '@tanstack/react-router';
import { ReceiptsPage } from '@/features/admin/ReceiptsPage';
import { validateReceiptsSearch } from '@/features/admin/receiptsSearch';

export const Route = createFileRoute('/admin/receipts')({
  validateSearch: validateReceiptsSearch,
  component: ReceiptsPage,
});
