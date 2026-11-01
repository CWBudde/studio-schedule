import { createFileRoute } from '@tanstack/react-router';
import { LevelsPage } from '@/features/admin/LevelsPage';

export const Route = createFileRoute('/admin/levels')({
  component: LevelsPage,
});
