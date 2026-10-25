/// <reference lib="webworker" />
/**
 * The service worker (ADR 0017), built by vite-plugin-pwa (injectManifest):
 *
 *  - precaches the app shell: the hashed assets, index.html, theme.js and
 *    the icons (never the demo's *.wasm or wasm_exec.js), so the app starts
 *    offline;
 *  - answers navigations with index.html, except the API, the calendar
 *    feeds and the manifest, which always go to the server;
 *  - never caches the API: its answers are `no-store`; the last schedule
 *    and My lessons come from the page's persisted query cache instead;
 *  - waits for the page to say "reload" (the update toast) before it
 *    replaces an older version;
 *  - shows web push notifications and opens their page on a click.
 */
import {
  cleanupOutdatedCaches,
  createHandlerBoundToURL,
  precacheAndRoute,
} from 'workbox-precaching';
import { NavigationRoute, registerRoute } from 'workbox-routing';
import { type NotificationData, notificationOptions, parsePushPayload } from './lib/pushPayload';

declare const self: ServiceWorkerGlobalScope;

precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();

/** The app's path, e.g. `/` or `/studio-schedule/`. */
const base = new URL(self.registration.scope).pathname;

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

registerRoute(
  new NavigationRoute(createHandlerBoundToURL('index.html'), {
    denylist: [
      new RegExp(`^${escape(base)}api(/|$)`),
      new RegExp(`^${escape(base)}ical/`),
      new RegExp(`^${escape(base)}manifest\\.webmanifest$`),
    ],
  }),
);

self.addEventListener('message', (event) => {
  if ((event.data as { type?: string } | null)?.type === 'SKIP_WAITING') void self.skipWaiting();
});

const icon = `${base}icons/icon-192.png`;

self.addEventListener('push', (event) => {
  let data: unknown = null;
  try {
    data = event.data?.json();
  } catch {
    data = event.data?.text() ?? null;
  }
  const payload = parsePushPayload(data);
  if (!payload) return;
  event.waitUntil(
    self.registration.showNotification(payload.title, notificationOptions(payload, icon)),
  );
});

/**
 * A click opens the notification's page: in a tab of the app that shows it
 * already, else in any tab of the app, else in a new window.
 */
self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const data = event.notification.data as NotificationData | null;
  const url = new URL(data?.url || base, self.location.origin).href;

  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      const same = windows.find((w) => w.url === url);
      if (same) {
        await same.focus();
        return;
      }
      const app = windows.find((w) => new URL(w.url).pathname.startsWith(base));
      if (app) {
        const focused = await app.focus();
        await focused.navigate(url).catch(() => self.clients.openWindow(url));
        return;
      }
      await self.clients.openWindow(url);
    })(),
  );
});
