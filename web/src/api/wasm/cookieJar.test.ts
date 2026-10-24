import { describe, expect, it } from 'vitest';
import { CookieJar, pathOnly } from './cookieJar';

function jarAt(start = Date.UTC(2026, 0, 1)) {
  let now = start;
  const jar = new CookieJar(() => now);
  return {
    jar,
    advance(ms: number) {
      now += ms;
    },
  };
}

describe('CookieJar', () => {
  it('stores a cookie and sends it back', () => {
    const { jar } = jarAt();
    jar.setCookie('session=abc123; Path=/; HttpOnly; SameSite=Lax', '/api/v1/auth/login');
    expect(jar.getCookieHeader('/api/v1/me')).toBe('session=abc123');
  });

  it('returns null when no cookie matches', () => {
    const { jar } = jarAt();
    expect(jar.getCookieHeader('/api/v1/health')).toBeNull();
    jar.setCookie('a=1; Path=/admin', '/');
    expect(jar.getCookieHeader('/api/v1/health')).toBeNull();
  });

  it('uses the RFC 6265 default path when Path is missing', () => {
    const { jar } = jarAt();
    jar.setCookie('a=1', '/api/v1/auth/login?next=/');
    expect(jar.getCookieHeader('/api/v1/auth/logout')).toBe('a=1');
    expect(jar.getCookieHeader('/api/v1/auth')).toBe('a=1');
    expect(jar.getCookieHeader('/api/v1/me')).toBeNull();

    jar.setCookie('root=1', '/login');
    expect(jar.getCookieHeader('/anything')).toBe('root=1');
  });

  it('matches paths on segment boundaries only', () => {
    const { jar } = jarAt();
    jar.setCookie('a=1; Path=/api', '/');
    expect(jar.getCookieHeader('/api')).toBe('a=1');
    expect(jar.getCookieHeader('/api/v1/x?y=1')).toBe('a=1');
    expect(jar.getCookieHeader('/apix')).toBeNull();
  });

  it('replaces a cookie with the same name and path', () => {
    const { jar } = jarAt();
    jar.setCookie('a=1; Path=/', '/');
    jar.setCookie('a=2; Path=/', '/');
    expect(jar.getCookieHeader('/')).toBe('a=2');
    expect(jar.size).toBe(1);
  });

  it('keeps cookies with the same name but different paths, longest path first', () => {
    const { jar } = jarAt();
    jar.setCookie('a=root; Path=/', '/');
    jar.setCookie('a=api; Path=/api', '/');
    jar.setCookie('b=1; Path=/', '/');
    expect(jar.getCookieHeader('/api/v1')).toBe('a=api; a=root; b=1');
  });

  it('deletes a cookie with Max-Age=0', () => {
    const { jar } = jarAt();
    jar.setCookie('session=abc; Path=/', '/');
    jar.setCookie('session=; Path=/; Max-Age=0', '/api/v1/auth/logout');
    expect(jar.getCookieHeader('/')).toBeNull();
    expect(jar.size).toBe(0);
  });

  it('deletes a cookie with an Expires date in the past', () => {
    const { jar } = jarAt();
    jar.setCookie('session=abc; Path=/', '/');
    jar.setCookie('session=; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT', '/');
    expect(jar.getCookieHeader('/')).toBeNull();
  });

  it('expires cookies after Max-Age seconds', () => {
    const { jar, advance } = jarAt();
    jar.setCookie('short=1; Path=/; Max-Age=60', '/');
    advance(59_000);
    expect(jar.getCookieHeader('/')).toBe('short=1');
    advance(1_000);
    expect(jar.getCookieHeader('/')).toBeNull();
  });

  it('honours Expires and lets Max-Age take precedence', () => {
    const start = Date.UTC(2026, 0, 1);
    const { jar, advance } = jarAt(start);
    jar.setCookie(`e=1; Path=/; Expires=${new Date(start + 10_000).toUTCString()}`, '/');
    jar.setCookie(
      `m=1; Path=/; Expires=${new Date(start + 10_000).toUTCString()}; Max-Age=3600`,
      '/',
    );
    advance(10_000);
    expect(jar.getCookieHeader('/')).toBe('m=1');
  });

  it('ignores Domain/Secure attributes and malformed headers', () => {
    const { jar } = jarAt();
    jar.setCookie('a=1; Domain=example.org; Secure; Path=/', '/');
    jar.setCookie('novalue', '/');
    jar.setCookie('=nameless', '/');
    expect(jar.getCookieHeader('/')).toBe('a=1');
  });

  it('keeps "=" characters inside values', () => {
    const { jar } = jarAt();
    jar.setCookie('token=a=b==; Path=/', '/');
    expect(jar.getCookieHeader('/')).toBe('token=a=b==');
  });

  it('saves its cookies and loads them into another jar', () => {
    const { jar, advance } = jarAt();
    jar.setCookie('old=1; Path=/', '/');
    jar.setCookie('session=abc; Path=/; Max-Age=60', '/');
    jar.setCookie('scoped=2; Path=/api', '/');

    const saved = jar.save();
    // What is saved survives structured cloning (IndexedDB).
    const copy = structuredClone(saved);

    let now = Date.UTC(2026, 0, 1);
    const other = new CookieJar(() => now);
    other.load(copy);
    expect(other.getCookieHeader('/api/v1/me')).toBe('scoped=2; old=1; session=abc');

    // Expiry and insertion order carry over.
    now += 61_000;
    expect(other.getCookieHeader('/')).toBe('old=1');
    other.setCookie('later=3; Path=/', '/');
    expect(other.getCookieHeader('/')).toBe('old=1; later=3');

    advance(61_000);
    expect(jar.save().map((c) => c.name)).toEqual(['old', 'scoped']);
  });

  it('clears all cookies', () => {
    const { jar } = jarAt();
    jar.setCookie('a=1; Path=/', '/');
    jar.clear();
    expect(jar.getCookieHeader('/')).toBeNull();
  });
});

describe('pathOnly', () => {
  it('strips query and fragment', () => {
    expect(pathOnly('/a/b?c=d#e')).toBe('/a/b');
    expect(pathOnly('/a#e')).toBe('/a');
    expect(pathOnly('')).toBe('/');
  });
});
