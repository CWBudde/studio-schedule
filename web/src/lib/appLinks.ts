/**
 * Links in text the app shows (e.g. mails in the demo mailbox): finding them
 * and telling apart links to the app itself, which should open within the app
 * instead of loading the page again.
 */

/** The page the app runs on: `window.location` in the browser. */
export interface Here {
  origin: string;
  pathname: string;
}

/**
 * The route of the app that url points to, as the router's history takes it
 * (e.g. '/confirm?token=x'), or null for a link elsewhere. With hash history
 * the route is in the hash of the app's own page; with browser history it is
 * any same-origin path.
 */
export function appPathOf(url: string, here: Here, history: 'hash' | 'browser'): string | null {
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return null;
  }
  if ((u.protocol !== 'http:' && u.protocol !== 'https:') || u.origin !== here.origin) return null;
  if (history === 'browser') return `${u.pathname}${u.search}${u.hash}`;
  if (u.pathname !== here.pathname || !u.hash.startsWith('#/')) return null;
  return u.hash.slice(1);
}

export type TextPart = { text: string } | { url: string };

// Punctuation that ends a sentence or encloses a link rather than belonging to it.
const urlPattern = /https?:\/\/[^\s<>"']*[^\s<>"'.,;:!?)\]}]/g;

/** Splits plain text into text and http(s) links. */
export function splitLinks(text: string): TextPart[] {
  const parts: TextPart[] = [];
  let last = 0;
  for (const match of text.matchAll(urlPattern)) {
    if (match.index > last) parts.push({ text: text.slice(last, match.index) });
    parts.push({ url: match[0] });
    last = match.index + match[0].length;
  }
  if (last < text.length) parts.push({ text: text.slice(last) });
  return parts;
}
