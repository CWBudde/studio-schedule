import { createFileRoute } from '@tanstack/react-router';
import { validateTeachSearch } from '@/features/teach/search';
import { TeachPage } from '@/features/teach/TeachPage';

export const Route = createFileRoute('/teach')({
  validateSearch: validateTeachSearch,
  component: TeachPage,
});
