/**
 * Minimal cookie jar for the in-browser (WASM) backend.
 *
 * The demo backend runs in a Web Worker and never goes through the browser's
 * network stack, so cookies set by the Go code (e.g. session cookies) must be
 * stored and replayed by hand. Everything is single-origin, therefore Domain,
 * Secure, HttpOnly and SameSite are accepted but ignored. Only Path, Max-Age
 * and Expires influence storage and matching (RFC 6265, simplified).
 */

interface StoredCookie {
  name: string;
  value: string;
  path: string;
  /** Epoch milliseconds, or null for a session cookie. */
  expiresAt: number | null;
  /** Insertion order, used as a tie breaker when serialising. */
  createdAt: number;
}

export class CookieJar {
  private readonly cookies = new Map<string, StoredCookie>();
  private counter = 0;
  private readonly now: () => number;

  constructor(now: () => number = Date.now) {
    this.now = now;
  }

  /**
   * Stores (or deletes) a cookie from a single Set-Cookie header value.
   * `requestPath` is the path of the request that produced the response and
   * determines the default cookie path.
   */
  setCookie(setCookieHeader: string, requestPath: string): void {
    const [pair = '', ...attributes] = setCookieHeader.split(';');
    const eq = pair.indexOf('=');
    if (eq <= 0) return; // no name: ignore (RFC 6265 allows it, we do not need it)

    const name = pair.slice(0, eq).trim();
    const value = pair.slice(eq + 1).trim();
    if (name === '') return;

    let path: string | null = null;
    let maxAge: number | null = null;
    let expires: number | null = null;

    for (const attribute of attributes) {
      const sep = attribute.indexOf('=');
      const key = (sep === -1 ? attribute : attribute.slice(0, sep)).trim().toLowerCase();
      const attrValue = sep === -1 ? '' : attribute.slice(sep + 1).trim();

      switch (key) {
        case 'path':
          path = attrValue.startsWith('/') ? attrValue : null;
          break;
        case 'max-age':
          if (/^-?\d+$/.test(attrValue)) maxAge = Number.parseInt(attrValue, 10);
          break;
        case 'expires': {
          const parsed = Date.parse(attrValue);
          if (!Number.isNaN(parsed)) expires = parsed;
          break;
        }
        default:
          break; // Domain, Secure, HttpOnly, SameSite, ...: irrelevant for a single origin
      }
    }

    const cookiePath = path ?? defaultPath(requestPath);
    const key = `${name}\u0000${cookiePath}`;

    // Max-Age wins over Expires (RFC 6265 5.3 step 3).
    let expiresAt: number | null = null;
    if (maxAge !== null) {
      expiresAt = maxAge <= 0 ? 0 : this.now() + maxAge * 1000;
    } else if (expires !== null) {
      expiresAt = expires;
    }

    if (expiresAt !== null && expiresAt <= this.now()) {
      this.cookies.delete(key);
      return;
    }

    const existing = this.cookies.get(key);
    this.cookies.set(key, {
      name,
      value,
      path: cookiePath,
      expiresAt,
      createdAt: existing?.createdAt ?? this.counter++,
    });
  }

  /** Returns the Cookie header value for a request to `requestPath`, or null. */
  getCookieHeader(requestPath: string): string | null {
    const path = pathOnly(requestPath);
    const now = this.now();
    const matching: StoredCookie[] = [];

    for (const [key, cookie] of this.cookies) {
      if (cookie.expiresAt !== null && cookie.expiresAt <= now) {
        this.cookies.delete(key);
        continue;
      }
      if (pathMatches(path, cookie.path)) matching.push(cookie);
    }

    if (matching.length === 0) return null;

    // Longer paths first, then older cookies first (RFC 6265 5.4 step 2).
    matching.sort((a, b) => b.path.length - a.path.length || a.createdAt - b.createdAt);
    return matching.map((c) => `${c.name}=${c.value}`).join('; ');
  }

  /** Number of (non-expired) cookies currently stored. Mainly for tests. */
  get size(): number {
    const now = this.now();
    let n = 0;
    for (const cookie of this.cookies.values()) {
      if (cookie.expiresAt === null || cookie.expiresAt > now) n++;
    }
    return n;
  }

  clear(): void {
    this.cookies.clear();
  }
}

/** Strips query string and fragment from a path such as `/api/v1/x?y=1`. */
export function pathOnly(pathWithQuery: string): string {
  const end = pathWithQuery.search(/[?#]/);
  const path = end === -1 ? pathWithQuery : pathWithQuery.slice(0, end);
  return path === '' ? '/' : path;
}

/** RFC 6265 5.1.4 default-path. */
function defaultPath(requestPath: string): string {
  const path = pathOnly(requestPath);
  if (!path.startsWith('/')) return '/';
  const lastSlash = path.lastIndexOf('/');
  if (lastSlash === 0) return '/';
  return path.slice(0, lastSlash);
}

/** RFC 6265 5.1.4 path-match. */
function pathMatches(requestPath: string, cookiePath: string): boolean {
  if (requestPath === cookiePath) return true;
  if (!requestPath.startsWith(cookiePath)) return false;
  return cookiePath.endsWith('/') || requestPath.charAt(cookiePath.length) === '/';
}
