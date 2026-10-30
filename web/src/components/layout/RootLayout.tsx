import { Link, Outlet, useMatches } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { startBackend } from '@/api/transport';
import { AppFooter } from '@/components/layout/AppFooter';
import { AppHeader } from '@/components/layout/AppHeader';
import { BackendGate } from '@/components/layout/BackendGate';
import { DemoBanner } from '@/components/layout/DemoBanner';
import { DemoNotices } from '@/components/layout/DemoNotices';
import { ImpersonationBanner } from '@/components/layout/ImpersonationBanner';
import { OfflineBanner } from '@/components/layout/OfflineBanner';
import { Button } from '@/components/ui/button';
import { Toaster } from '@/components/ui/sonner';
import { AnnouncementBanner } from '@/features/announcements/AnnouncementBanner';
import { markDemoEntered } from '@/features/demo/entered';
import { usePushKeepAlive } from '@/features/push/usePushKeepAlive';
import { isDemo } from '@/lib/env';

/** The app around every page, or nothing around a bare one (the demo's about page). */
export function RootShell() {
  const bare = useMatches({ select: (matches) => matches.some((m) => m.staticData.bare) });
  return (
    <>
      {isDemo && bare ? <Outlet /> : <RootLayout />}
      <Toaster />
    </>
  );
}

function RootLayout() {
  const { t } = useTranslation();
  usePushKeepAlive();
  useEffect(() => {
    // In the demo build this boots the Go/WASM backend in a Web Worker, and
    // from now on the demo opens without its about page.
    startBackend();
    if (isDemo) markDemoEntered();
  }, []);
  return (
    <div className="flex min-h-svh flex-col">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:top-2 focus:left-2"
      >
        {t('a11y.skipToContent')}
      </a>
      <DemoBanner />
      <OfflineBanner />
      <ImpersonationBanner />
      <AppHeader />
      <AnnouncementBanner />
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
      <DemoNotices />
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
