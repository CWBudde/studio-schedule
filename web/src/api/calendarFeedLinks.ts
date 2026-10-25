import { transport } from '@/lib/env';
import { openDownload } from './download';
import type { NewCalendarFeed } from './queries';

/**
 * The address a calendar app subscribes to, or null where no calendar app
 * can reach the feed: the web demo has no server, only its download works.
 * Elsewhere the API's absolute URL, or, if the server does not know its own
 * URL, the path below this page's origin (the API is served from there).
 */
export function subscriptionUrl(created: NewCalendarFeed): string | null {
  if (transport === 'wasm') return null;
  return created.url ?? new URL(created.path, window.location.origin).href;
}

/** The subscription address as a webcal:// link, which opens the system's calendar app. */
export function webcalUrl(url: string): string {
  return url.replace(/^https?:/i, 'webcal:');
}

/**
 * Downloads the feed's current document as an .ics file. Over HTTP from the
 * feed's absolute URL when the server knows it (it has the app's base
 * path), else from the path; the web demo has only the path, which the
 * worker bridge answers.
 */
export function downloadCalendarFeed(created: NewCalendarFeed): Promise<void> {
  const address = transport === 'wasm' ? created.path : (created.url ?? created.path);
  return openDownload(`${address}${address.includes('?') ? '&' : '?'}download=true`);
}
