import { createFileRoute } from '@tanstack/react-router';
import { TermsPage } from '@/features/admin/TermsPage';

export const Route = createFileRoute('/admin/terms/')({
  component: TermsPage,
});
