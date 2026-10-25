import { notificationOptions, parsePushPayload } from '@/lib/pushPayload';

/**
 * Shows a notification of the web demo (ADR 0017). Its backend runs in this
 * page's worker and has no push service to send through: the demo's push
 * sender (internal/wasmbridge) hands the payload to the page, which shows it
 * through the service worker like a real push, so a click opens its page
 * the same way. Nothing shows without the permission.
 */
export async function showDemoNotification(payload: string): Promise<void> {
  const p = parsePushPayload(payload);
  if (!p || typeof Notification === 'undefined' || Notification.permission !== 'granted') return;
  const reg = await navigator.serviceWorker?.getRegistration();
  if (!reg) return;
  await reg.showNotification(
    p.title,
    notificationOptions(p, `${import.meta.env.BASE_URL}icons/icon-192.png`),
  );
}
