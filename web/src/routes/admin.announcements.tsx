import { createFileRoute } from '@tanstack/react-router';
import { AnnouncementsPage } from '@/features/announcements/AnnouncementsPage';

export const Route = createFileRoute('/admin/announcements')({
  component: AnnouncementsPage,
});
