import { createFileRoute, redirect } from '@tanstack/react-router';

// The first section; the office and admins may both see it, and the layout
// tells everyone else that the administration is not for them.
export const Route = createFileRoute('/admin/')({
  beforeLoad: () => {
    throw redirect({ to: '/admin/locations', replace: true });
  },
});
