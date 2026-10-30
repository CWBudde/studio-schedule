import { Link, Outlet, useMatches } from '@tanstack/react-router';
import { useEffect, useLayoutEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { startBackend } from '@/api/transport';
import { AppFooter } from '@/components/layout/AppFooter';
import { useShellHeader } from '@/components/layout/AppHeader';
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
import { ThemeProvider } from '@/themes/ThemeProvider';
import { useSlot } from '@/themes/useSlot';

/** The app around every page, or nothing around a bare one (the demo's about page). */
export function RootShell() {
  const bare = useMatches({ select: (matches) => matches.some((m) => m.staticData.bare) });
  const bareDemo = isDemo && bare;
  useLayoutEffect(() => {
    // A bare page keeps its own look: no theme from the cache (web/public/theme.js).
    if (bareDemo) delete document.documentElement.dataset.theme;
  }, [bareDemo]);
  return (
    <>
      {bareDemo ? <Outlet /> : <RootLayout />}
      <Toaster />
    </>
  );
}

function RootLayout() {
  usePushKeepAlive();
  useEffect(() => {
    // In the demo build this boots the Go/WASM backend in a Web Worker, and
    // from now on the demo opens without its about page.
    startBackend();
    if (isDemo) markDemoEntered();
  }, []);
  return (
    <>
      <ThemeProvider>
        <ThemedLayout />
      </ThemeProvider>
      {/* Outside the theme: it renders nothing, and listens while a theme loads too. */}
      <DemoNotices />
    </>
  );
}

/** The theme's Shell slot around the page. */
function ThemedLayout() {
  const Shell = useSlot('Shell');
  const header = useShellHeader();
  return (
    // The slot changes only with the theme, and then remounting what it renders is intended.
    // eslint-disable-next-line react-hooks/static-components
    <Shell
      banners={
        <>
          <DemoBanner />
          <OfflineBanner />
          <ImpersonationBanner />
        </>
      }
      header={header}
      announcements={<AnnouncementBanner />}
      footer={<AppFooter />}
    >
      <BackendGate>
        <Outlet />
      </BackendGate>
    </Shell>
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
