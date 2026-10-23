import { createFileRoute } from '@tanstack/react-router';
import { TimetablePage } from '@/features/timetable/TimetablePage';
import { validateTimetableSearch } from '@/features/timetable/search';

export const Route = createFileRoute('/classes')({
  validateSearch: validateTimetableSearch,
  component: TimetablePage,
});
