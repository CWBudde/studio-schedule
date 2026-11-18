import { toast } from 'sonner';
import { Workbox } from 'workbox-window';
import i18n from '@/i18n';
import { appPath } from '@/lib/env';

/**
 * The service worker (src/sw.ts, ADR 0017): it makes the app start offline
 * and installable. A new version waits until the visitor reloads from the
 * toast, so a page never runs half old, half new code.
 */
export function registerServiceWorker(): void {
  if (import.meta.env.DEV || typeof navigator === 'undefined' || !('serviceWorker' in navigator))
    return;

  const worker = new Workbox(`${appPath}sw.js`, { scope: appPath });
  worker.addEventListener('waiting', () => {
    worker.addEventListener('controlling', () => window.location.reload());
    toast(i18n.t('pwa.updateReady'), {
      id: 'pwa-update',
      duration: Infinity,
      action: { label: i18n.t('pwa.reload'), onClick: () => worker.messageSkipWaiting() },
    });
  });
  void worker.register().catch((err: unknown) => {
    console.warn('[studio-schedule] service worker registration failed', err);
  });
}
