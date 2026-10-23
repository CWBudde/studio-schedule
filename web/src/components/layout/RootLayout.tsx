import { Link, Outlet } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { AppFooter } from '@/components/layout/AppFooter';
import { AppHeader } from '@/components/layout/AppHeader';
import { BackendGate } from '@/components/layout/BackendGate';
import { DemoBanner } from '@/components/layout/DemoBanner';
import { Button } from '@/components/ui/button';
import { Toaster } from '@/components/ui/sonner';

export function RootLayout() {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-svh flex-col">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:top-2 focus:left-2"
      >
        {t('a11y.skipToContent')}
      </a>
      <DemoBanner />
      <AppHeader />
      <main
        id="main"
        tabIndex={-1}
        className="mx-auto w-full max-w-5xl flex-1 px-4 py-8 outline-none"
      >
        <BackendGate>
          <Outlet />
        </BackendGate>
      </main>
      <AppFooter />
      <Toaster />
    </div>
  );
}

export function NotFound() {
  const { t } = useTranslation();
  return (
    <div className="space-y-4 py-12 text-center">
      <h1 className="font-heading text-2xl font-semibold">{t('notFound.title')}</h1>
      <p className="text-muted-foreground">{t('notFound.description')}</p>
      <Button asChild variant="outline">
        <Link to="/">{t('notFound.backHome')}</Link>
      </Button>
    </div>
  );
}
