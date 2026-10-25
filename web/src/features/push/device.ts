import { isDemo } from '@/lib/env';

/**
 * This device's web push subscription (ADR 0017). In the browser, the
 * service worker's PushManager subscribes with the server's VAPID key. The
 * web demo has no push service to reach: its "subscription" is made up and
 * kept in localStorage, and the demo backend shows its notifications on
 * this page (src/pwa.ts). Either way the server stores it like any other.
 */

/** A subscription as the API takes it (PushSubscription.toJSON()). */
export interface DeviceSubscription {
  endpoint: string;
  keys: { p256dh: string; auth: string };
}

export interface PushDevice {
  /** Whether this browser can get notifications at all. */
  readonly supported: boolean;
  /** The permission of this site to show notifications. */
  permission(): NotificationPermission;
  requestPermission(): Promise<NotificationPermission>;
  /** This device's subscription, if it has one. */
  current(): Promise<DeviceSubscription | null>;
  /** Subscribes this device (with the VAPID public key, for a real push service). */
  subscribe(publicKey: string | undefined): Promise<DeviceSubscription>;
  /** Ends this device's subscription and returns it; null if there was none. */
  unsubscribe(): Promise<DeviceSubscription | null>;
}

/** Thrown when the browser has no service worker to subscribe with (e.g. the dev server). */
export class NoServiceWorkerError extends Error {
  constructor() {
    super('No service worker is registered');
    this.name = 'NoServiceWorkerError';
  }
}

function hasNotifications(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window;
}

function hasServiceWorker(): boolean {
  return typeof navigator !== 'undefined' && 'serviceWorker' in navigator;
}

const permission = (): NotificationPermission =>
  hasNotifications() ? Notification.permission : 'denied';

const requestPermission = (): Promise<NotificationPermission> =>
  hasNotifications() ? Notification.requestPermission() : Promise.resolve('denied');

async function registration(): Promise<ServiceWorkerRegistration> {
  const reg = hasServiceWorker() ? await navigator.serviceWorker.getRegistration() : undefined;
  if (!reg) throw new NoServiceWorkerError();
  return reg;
}

export function base64UrlToBytes(s: string): Uint8Array<ArrayBuffer> {
  const b64 = s
    .replace(/-/g, '+')
    .replace(/_/g, '/')
    .padEnd(Math.ceil(s.length / 4) * 4, '=');
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function bytesToBase64Url(bytes: ArrayBuffer | Uint8Array): string {
  let bin = '';
  for (const b of new Uint8Array(bytes)) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function fromJSON(json: PushSubscriptionJSON): DeviceSubscription {
  return {
    endpoint: json.endpoint ?? '',
    keys: { p256dh: json.keys?.p256dh ?? '', auth: json.keys?.auth ?? '' },
  };
}

function browserDevice(): PushDevice {
  return {
    supported: hasNotifications() && hasServiceWorker() && 'PushManager' in window,
    permission,
    requestPermission,
    async current() {
      if (!hasServiceWorker()) return null;
      const reg = await navigator.serviceWorker.getRegistration();
      const sub = await reg?.pushManager.getSubscription();
      return sub ? fromJSON(sub.toJSON()) : null;
    },
    async subscribe(publicKey) {
      if (!publicKey) throw new Error('No VAPID key');
      const reg = await registration();
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: base64UrlToBytes(publicKey),
      });
      return fromJSON(sub.toJSON());
    },
    async unsubscribe() {
      if (!hasServiceWorker()) return null;
      const reg = await navigator.serviceWorker.getRegistration();
      const sub = await reg?.pushManager.getSubscription();
      if (!sub) return null;
      const json = fromJSON(sub.toJSON());
      await sub.unsubscribe();
      return json;
    },
  };
}

const DEMO_KEY = 'studio-schedule-demo-push';

function readDemo(): DeviceSubscription | null {
  try {
    const raw = localStorage.getItem(DEMO_KEY);
    return raw ? (JSON.parse(raw) as DeviceSubscription) : null;
  } catch {
    return null;
  }
}

/** The web demo's made-up subscription: a real P-256 key and secret, an endpoint nobody serves. */
export function demoDevice(): PushDevice {
  return {
    supported: hasNotifications() && hasServiceWorker(),
    permission,
    requestPermission,
    current: () => Promise.resolve(readDemo()),
    async subscribe() {
      await registration(); // the page shows the notifications through it
      const pair = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, [
        'deriveBits',
      ]);
      const sub: DeviceSubscription = {
        endpoint: `https://push.invalid/demo/${crypto.randomUUID()}`,
        keys: {
          p256dh: bytesToBase64Url(await crypto.subtle.exportKey('raw', pair.publicKey)),
          auth: bytesToBase64Url(crypto.getRandomValues(new Uint8Array(16))),
        },
      };
      localStorage.setItem(DEMO_KEY, JSON.stringify(sub));
      return sub;
    },
    unsubscribe() {
      const sub = readDemo();
      try {
        localStorage.removeItem(DEMO_KEY);
      } catch {
        // Nothing saved, nothing to remove.
      }
      return Promise.resolve(sub);
    },
  };
}

/** This device. */
export const pushDevice: PushDevice = isDemo ? demoDevice() : browserDevice();

const OWNER_KEY = 'studio-schedule-push-owner';

/**
 * The account that turned notifications on here. The browser keeps a
 * subscription across sign-ins; it counts as on only for that account.
 */
export function pushOwner(): string | null {
  try {
    return localStorage.getItem(OWNER_KEY);
  } catch {
    return null;
  }
}

export function setPushOwner(accountId: string | null): void {
  try {
    if (accountId) localStorage.setItem(OWNER_KEY, accountId);
    else localStorage.removeItem(OWNER_KEY);
  } catch {
    // Without storage the switch shows off after a reload.
  }
}

/**
 * A short name of this device for its holder, e.g. "Firefox · Linux",
 * from the user agent (best effort; product names stay untranslated).
 */
export function deviceLabel(
  ua: string = typeof navigator === 'undefined' ? '' : navigator.userAgent,
): string {
  const first = (names: [RegExp, string][]) => names.find(([re]) => re.test(ua))?.[1];
  const browser =
    first([
      [/Edg\//, 'Edge'],
      [/OPR\//, 'Opera'],
      [/Firefox\//, 'Firefox'],
      [/Chrome\//, 'Chrome'],
      [/Safari\//, 'Safari'],
    ]) ?? 'Browser';
  const os = first([
    [/Android/, 'Android'],
    [/iPhone|iPad|iPod/, 'iOS'],
    [/CrOS/, 'ChromeOS'],
    [/Windows/, 'Windows'],
    [/Mac OS X|Macintosh/, 'macOS'],
    [/Linux/, 'Linux'],
  ]);
  return os ? `${browser} · ${os}` : browser;
}
