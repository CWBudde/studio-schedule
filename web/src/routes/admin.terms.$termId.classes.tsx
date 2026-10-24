import { createFileRoute } from '@tanstack/react-router';
import { ClassesRoute } from '@/features/admin/ClassesPage';

export const Route = createFileRoute('/admin/terms/$termId/classes')({
  component: ClassesRoute,
});
