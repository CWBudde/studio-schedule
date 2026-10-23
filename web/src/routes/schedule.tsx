import { createFileRoute } from '@tanstack/react-router';
import { SchedulePage } from '@/features/schedule/SchedulePage';
import { validateScheduleSearch } from '@/features/schedule/search';

export const Route = createFileRoute('/schedule')({
  validateSearch: validateScheduleSearch,
  component: SchedulePage,
});
