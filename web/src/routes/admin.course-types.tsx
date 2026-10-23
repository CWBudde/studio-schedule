import { createFileRoute } from '@tanstack/react-router';
import { CourseTypesPage } from '@/features/admin/CourseTypesPage';

export const Route = createFileRoute('/admin/course-types')({
  component: CourseTypesPage,
});
