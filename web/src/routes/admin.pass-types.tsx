import { createFileRoute } from '@tanstack/react-router';
import { PassTypesPage } from '@/features/admin/PassTypesPage';

export const Route = createFileRoute('/admin/pass-types')({
  component: PassTypesPage,
});
