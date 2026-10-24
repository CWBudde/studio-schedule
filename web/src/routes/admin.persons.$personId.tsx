import { createFileRoute } from '@tanstack/react-router';
import { PersonRoute } from '@/features/admin/PersonPage';

export const Route = createFileRoute('/admin/persons/$personId')({
  component: PersonRoute,
});
