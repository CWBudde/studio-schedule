import { createFileRoute } from '@tanstack/react-router';
import { StaffPage } from '@/features/admin/StaffPage';

export const Route = createFileRoute('/admin/staff')({
  component: StaffPage,
});
