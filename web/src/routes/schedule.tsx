import { createFileRoute } from '@tanstack/react-router';
import { validateScheduleSearch } from '@/features/schedule/search';

export const Route = createFileRoute('/schedule')({
  validateSearch: validateScheduleSearch,
});
