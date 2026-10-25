import { toast } from 'sonner';
import { registerSW } from 'virtual:pwa-register';
import i18n from '@/i18n';

/**
 * The service worker (src/sw.ts, ADR 0017): it makes the app start offline
 * and installable. A new version waits until the visitor reloads from the
 * toast, so a page never runs half old, half new code.
 */
export function registerServiceWorker(): void {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return;

  const update = registerSW({
    onNeedRefresh() {
      toast(i18n.t('pwa.updateReady'), {
        id: 'pwa-update',
        duration: Infinity,
        action: { label: i18n.t('pwa.reload'), onClick: () => void update(true) },
      });
    },
    onRegisterError(err: unknown) {
      console.warn('[studio-schedule] service worker registration failed', err);
    },
  });
}
