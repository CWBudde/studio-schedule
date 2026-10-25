import { createFileRoute } from '@tanstack/react-router';
import { MyLessonsPage } from '@/features/my-lessons/MyLessonsPage';
import { validateMyLessonsSearch } from '@/features/my-lessons/search';

export const Route = createFileRoute('/my-lessons')({
  validateSearch: validateMyLessonsSearch,
  component: MyLessonsPage,
});
