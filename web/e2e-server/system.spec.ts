import { accounts, expect, seedPassword, test } from './fixtures';

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

test('the health probes answer and every answer carries a request ID', async ({ request }) => {
  for (const path of ['/healthz', '/readyz']) {
    const res = await request.get(path);
    expect(res.status(), path).toBe(200);
    expect(await res.json()).toEqual({ status: 'ok' });
    expect(res.headers()['x-request-id'], path).toMatch(uuid);
  }

  // A client that is no trusted proxy cannot choose its request ID.
  const spoofed = '01890a5d-ac96-774b-bcce-b302099a8057';
  const res = await request.get('/api/v1/health', { headers: { 'X-Request-Id': spoofed } });
  expect(res.status()).toBe(200);
  expect(res.headers()['x-request-id']).toMatch(uuid);
  expect(res.headers()['x-request-id']).not.toBe(spoofed);

  // A problem names the request, so it can be found in the log.
  const missing = await request.get('/api/v1/no-such-route');
  expect(missing.status()).toBe(404);
  expect(missing.headers()['content-type']).toContain('application/problem+json');
  expect((await missing.json()).requestId).toBe(missing.headers()['x-request-id']);
});

test('a cross-origin request that changes something is refused', async ({ request, baseURL }) => {
  const login = {
    data: { email: accounts.member.email, password: seedPassword },
  };

  // From another site, as a forged form or fetch in a victim's browser.
  const forged = await request.post('/api/v1/auth/login', {
    ...login,
    headers: { Origin: 'https://attacker.example.test', 'Sec-Fetch-Site': 'cross-site' },
  });
  expect(forged.status()).toBe(403);
  expect(forged.headers()['content-type']).toContain('application/problem+json');
  expect((await forged.json()).detail).toBe('cross-origin request rejected');
  expect(forged.headers()['set-cookie']).toBeUndefined();

  // An Origin of another site without Fetch metadata (older browsers).
  const originOnly = await request.post('/api/v1/auth/logout', {
    headers: { Origin: 'https://attacker.example.test' },
  });
  expect(originOnly.status()).toBe(403);

  // The app's own origin passes, and the session cookie is HttpOnly and
  // SameSite=Lax.
  const own = await request.post('/api/v1/auth/login', {
    ...login,
    headers: { Origin: baseURL!, 'Sec-Fetch-Site': 'same-origin' },
  });
  expect(own.status()).toBe(200);
  const cookie = own.headers()['set-cookie'] ?? '';
  expect(cookie).toMatch(/^studio_session=/);
  expect(cookie).toContain('HttpOnly');
  expect(cookie).toContain('SameSite=Lax');
});

test('the server sends the security headers', async ({ request }) => {
  const res = await request.get('/');
  expect(res.status()).toBe(200);
  const headers = res.headers();
  expect(headers['content-security-policy']).toContain("default-src 'self'");
  expect(headers['content-security-policy']).not.toContain("'unsafe-eval'");
  expect(headers['x-content-type-options']).toBe('nosniff');
  expect(headers['x-frame-options']).toBe('DENY');
  expect(headers['referrer-policy']).toBe('same-origin');

  // The API's answers are never cached.
  const api = await request.get('/api/v1/health');
  expect(api.headers()['cache-control']).toBe('no-store');
});
