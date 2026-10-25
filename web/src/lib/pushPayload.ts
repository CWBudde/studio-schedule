/**
 * The JSON of a web push notification (ADR 0017), as the server queues it
 * (internal/service/push.go): what it shows and the page a click opens. The
 * service worker shows it for a real push; the web demo's page shows it
 * itself (src/pwa.ts). Shared by both, so it imports nothing.
 */
export interface PushPayload {
  title: string;
  body: string;
  /** The page a click opens. */
  url: string;
  /** Replaces an earlier notification of the same subject. */
  tag: string;
  lang: string;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

/** Reads a payload; null for anything without a title. */
export function parsePushPayload(data: unknown): PushPayload | null {
  if (typeof data === 'string') {
    try {
      return parsePushPayload(JSON.parse(data));
    } catch {
      return null;
    }
  }
  if (!data || typeof data !== 'object') return null;
  const o = data as Record<string, unknown>;
  const title = text(o.title).trim();
  if (!title) return null;
  return { title, body: text(o.body), url: text(o.url), tag: text(o.tag), lang: text(o.lang) };
}

/** What a notification click needs to know. */
export interface NotificationData {
  url: string;
}

/** The options of `showNotification` for a payload, with the app's icon. */
export function notificationOptions(p: PushPayload, icon: string): NotificationOptions {
  const data: NotificationData = { url: p.url };
  return {
    body: p.body,
    icon,
    badge: icon,
    lang: p.lang || undefined,
    tag: p.tag || undefined,
    data,
  };
}
