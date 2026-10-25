import { createFileRoute } from '@tanstack/react-router';
import { StatisticsPage } from '@/features/admin/StatisticsPage';
import { validateStatisticsSearch } from '@/features/admin/statisticsSearch';

export const Route = createFileRoute('/admin/statistics')({
  validateSearch: validateStatisticsSearch,
  component: StatisticsPage,
});
