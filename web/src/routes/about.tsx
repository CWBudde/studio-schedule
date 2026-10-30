import { createFileRoute, notFound } from '@tanstack/react-router';
import { AboutPage } from '@/features/demo/AboutPage';
import { isDemo } from '@/lib/env';

// The web demo's entrance, without the app around it: it starts no backend.
export const Route = createFileRoute('/about')({
  staticData: { bare: true },
  beforeLoad: () => {
    if (!isDemo) throw notFound();
  },
  component: AboutPage,
});
