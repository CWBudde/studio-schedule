import { createFileRoute, redirect } from '@tanstack/react-router';
import { hasEnteredDemo } from '@/features/demo/entered';
import { HomePage } from '@/features/home/HomePage';
import { isDemo } from '@/lib/env';

export const Route = createFileRoute('/')({
  // The web demo opens with its about page until the visitor enters it.
  beforeLoad: () => {
    if (isDemo && !hasEnteredDemo()) throw redirect({ to: '/about', replace: true });
  },
  component: HomePage,
});
