import { accounts, expect, seedPassword, test } from './fixtures';

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

test('the health probes answer and every answer carries a request ID', async ({ request }) => {
  for (const path of ['./healthz', './readyz']) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(200);
    expect(await res.json()).toEqual({ status: 'ok' });
    expect(res.headers()['x-request-id'], path).toMatch(uuid);
  }

  // A client that is no trusted proxy cannot choose its request ID.
  const spoofed = '01890a5d-ac96-774b-bcce-b302099a8057';
  const res = await request.get('./api/v1/health', { headers: { 'X-Request-Id': spoofed } });
  expect(res.status()).toBe(200);
  expect(res.headers()['x-request-id']).toMatch(uuid);
  expect(res.headers()['x-request-id']).not.toBe(spoofed);

  // A problem names the request, so it can be found in the log.
  const missing = await request.get('./api/v1/no-such-route');
  expect(missing.status()).toBe(404);
  expect(missing.headers()['content-type']).toContain('application/problem+json');
  expect((await missing.json()).requestId).toBe(missing.headers()['x-request-id']);
});

test('a cross-origin request that changes something is refused', async ({ request, baseURL }) => {
  const login = {
    data: { email: accounts.member.email, password: seedPassword },
  };

  // From another site, as a forged form or fetch in a victim's browser.
  const forged = await request.post('./api/v1/auth/login', {
    ...login,
    headers: { Origin: 'https://attacker.example.test', 'Sec-Fetch-Site': 'cross-site' },
  });
  expect(forged.status()).toBe(403);
  expect(forged.headers()['content-type']).toContain('application/problem+json');
  expect((await forged.json()).detail).toBe('cross-origin request rejected');
  expect(forged.headers()['set-cookie']).toBeUndefined();

  // An Origin of another site without Fetch metadata (older browsers).
  const originOnly = await request.post('./api/v1/auth/logout', {
    headers: { Origin: 'https://attacker.example.test' },
  });
  expect(originOnly.status()).toBe(403);

  // The app's own origin passes, and the session cookie is HttpOnly and
  // SameSite=Lax.
  const own = await request.post('./api/v1/auth/login', {
    ...login,
    headers: { Origin: new URL(baseURL!).origin, 'Sec-Fetch-Site': 'same-origin' },
  });
  expect(own.status()).toBe(200);
  const cookie = own.headers()['set-cookie'] ?? '';
  expect(cookie).toMatch(/^studio_session=/);
  expect(cookie).toContain(`Path=${new URL(baseURL!).pathname.replace(/\/$/, '') || '/'};`);
  expect(cookie).toContain('HttpOnly');
  expect(cookie).toContain('SameSite=Lax');
});

test('the server sends the security headers', async ({ request }) => {
  const res = await request.get('./');
  expect(res.status()).toBe(200);
  const headers = res.headers();
  expect(headers['content-security-policy']).toContain("default-src 'self'");
  expect(headers['content-security-policy']).not.toContain("'unsafe-eval'");
  expect(headers['x-content-type-options']).toBe('nosniff');
  expect(headers['x-frame-options']).toBe('DENY');
  expect(headers['referrer-policy']).toBe('same-origin');

  // The API's answers are never cached.
  const api = await request.get('./api/v1/health');
  expect(api.headers()['cache-control']).toBe('no-store');
});

test('the server makes the app installable from the school settings', async ({
  baseURL,
  page,
  request,
}) => {
  // The manifest carries the seeded school's name, and the page links it.
  const settings = await (await request.get('./api/v1/settings/public')).json();
  const res = await request.get('./manifest.webmanifest');
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('application/manifest+json');
  const manifest = await res.json();
  expect(manifest.name).toBe(settings.schoolName);
  const base = new URL(baseURL!).pathname;
  if (base !== '/') {
    expect((await request.get('/api/v1/health')).status()).toBe(404);
    const redirected = await request.get(`${base.slice(0, -1)}?view=week`, { maxRedirects: 0 });
    expect(redirected.status()).toBe(308);
    expect(redirected.headers()['location']).toBe(`${base}?view=week`);
  }
  expect(manifest).toMatchObject({ id: base, start_url: base, scope: base, display: 'standalone' });
  for (const { src } of manifest.icons as { src: string }[]) {
    expect(src).toMatch(new RegExp(`^${base}icons/`));
    expect((await request.get(src)).headers()['content-type']).toBe('image/png');
  }

  await page.goto('./login');
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in' })).toBeVisible();
  const signInURL = page.url();
  const skip = page.getByRole('link', { name: 'Skip to content' });
  await skip.focus();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('main')).toBeFocused();
  await expect(page).toHaveURL(signInURL);
  await expect(page.locator('base')).toHaveAttribute('href', base);
  await expect(page.locator('link[rel="manifest"]')).toHaveAttribute(
    'href',
    './manifest.webmanifest',
  );

  // The policy names the service worker and the manifest, both the app's own.
  const csp = (await request.get('./')).headers()['content-security-policy'] ?? '';
  expect(csp).toContain("worker-src 'self'");
  expect(csp).toContain("manifest-src 'self'");

  // The service worker is revalidated on every load, and registers.
  const sw = await request.get('./sw.js');
  expect(sw.status()).toBe(200);
  expect(sw.headers()['cache-control']).toBe('no-cache');
  expect(sw.headers()['content-type']).toContain('text/javascript');
  const scope = await page.evaluate(async () => (await navigator.serviceWorker.ready).scope);
  expect(new URL(scope).pathname).toBe(base);

  await page.reload();
  await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);
  await page.context().setOffline(true);
  try {
    await page.goto('./login?offline=1');
    await expect(page.getByRole('heading', { level: 1, name: 'Sign in' })).toBeVisible();
  } finally {
    await page.context().setOffline(false);
  }
});
