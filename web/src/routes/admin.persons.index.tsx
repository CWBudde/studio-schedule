import { createFileRoute } from '@tanstack/react-router';
import { PersonsPage } from '@/features/admin/PersonsPage';
import { validatePersonsSearch } from '@/features/admin/personsSearch';

export const Route = createFileRoute('/admin/persons/')({
  validateSearch: validatePersonsSearch,
  component: PersonsPage,
});
