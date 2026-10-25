import { createFileRoute } from '@tanstack/react-router';
import { ImportPage } from '@/features/import/ImportPage';

export const Route = createFileRoute('/admin/import')({
  component: ImportPage,
});
