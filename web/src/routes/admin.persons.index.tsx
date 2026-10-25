import { createFileRoute } from '@tanstack/react-router';
import { PersonsPage } from '@/features/admin/PersonsPage';

// The search is history state (personsSearch.ts), not a search param.
export const Route = createFileRoute('/admin/persons/')({
  component: PersonsPage,
});
