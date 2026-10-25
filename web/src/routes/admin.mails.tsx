import { createFileRoute } from '@tanstack/react-router';
import { MailLogPage } from '@/features/mail-log/MailLogPage';

export const Route = createFileRoute('/admin/mails')({
  component: MailLogPage,
});
