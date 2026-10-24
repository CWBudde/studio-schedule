import { describe, expect, it, vi } from 'vitest';
import { CookieJar } from './cookieJar';
import { createRequestHandler, toStandaloneBuffer } from './host';
import type {
  GoBridgeAPI,
  GoBridgeRequest,
  GoBridgeResponse,
  WorkerRequestMessage,
} from './protocol';

const encoder = new TextEncoder();

function request(overrides: Partial<WorkerRequestMessage> = {}): WorkerRequestMessage {
  return {
    type: 'request',
    id: 1,
    method: 'GET',
    url: '/api/v1/me',
    headers: [],
    body: null,
    ...overrides,
  };
}

describe('createRequestHandler', () => {
  it('stores Set-Cookie from Go responses and replays the cookie', async () => {
    const seen: GoBridgeRequest[] = [];
    const api: GoBridgeAPI = {
      fetch: vi.fn(async (req: GoBridgeRequest): Promise<GoBridgeResponse> => {
        seen.push(req);
        if (req.url === '/api/v1/auth/login') {
          return {
            status: 204,
            headers: [
              ['Set-Cookie', 'session=s1; Path=/; HttpOnly'],
              ['X-Trace', 't1'],
            ],
            body: null,
          };
        }
        return {
          status: 200,
          headers: [['Content-Type', 'application/json']],
          body: encoder.encode('{}'),
        };
      }),
    };
    const handle = createRequestHandler(api, new CookieJar());

    const login = await handle(request({ method: 'post', url: '/api/v1/auth/login' }));
    expect(login).toMatchObject({ type: 'response', status: 204, headers: [['X-Trace', 't1']] });

    // A cookie header sent by the caller is replaced by the jar's cookies.
    await handle(
      request({
        id: 2,
        headers: [
          ['Cookie', 'forged=1'],
          ['Accept', 'application/json'],
        ],
      }),
    );
    expect(seen[0]?.method).toBe('POST');
    expect(seen[1]?.headers).toEqual([
      ['Accept', 'application/json'],
      ['cookie', 'session=s1'],
    ]);
  });

  it('passes the request body as Uint8Array', async () => {
    const captured: { body: Uint8Array | null } = { body: null };
    const api: GoBridgeAPI = {
      fetch: async (req) => {
        captured.body = req.body;
        return { status: 201, headers: {}, body: new Uint8Array() };
      },
    };
    const body = encoder.encode('{"a":1}').buffer;
    await createRequestHandler(api)(request({ method: 'POST', body }));
    expect(captured.body).not.toBeNull();
    expect(new TextDecoder().decode(captured.body ?? undefined)).toBe('{"a":1}');
  });

  it('reports responses that set cookies', async () => {
    const api: GoBridgeAPI = {
      fetch: async (req) => ({
        status: 200,
        headers: req.url === '/api/v1/auth/me' ? [['Set-Cookie', 'session=s2; Path=/']] : [],
        body: null,
      }),
    };
    const onCookies = vi.fn();
    const handle = createRequestHandler(api, new CookieJar(), onCookies);

    await handle(request({ method: 'GET', url: '/api/v1/schedule' }));
    await handle(request({ method: 'POST', url: '/api/v1/admin/locations' }));
    expect(onCookies).not.toHaveBeenCalled();

    await handle(request({ method: 'GET', url: '/api/v1/auth/me' }));
    expect(onCookies).toHaveBeenCalledTimes(1);
  });

  it('turns Go errors into error replies', async () => {
    const api: GoBridgeAPI = {
      fetch: () => Promise.reject(new Error('boom')),
    };
    await expect(createRequestHandler(api)(request({ id: 7 }))).resolves.toEqual({
      type: 'error',
      id: 7,
      message: 'boom',
    });
  });
});

describe('toStandaloneBuffer', () => {
  it('returns the underlying buffer when the view covers it', () => {
    const bytes = new Uint8Array([1, 2, 3]);
    expect(toStandaloneBuffer(bytes)).toBe(bytes.buffer);
  });

  it('copies partial views', () => {
    const backing = new Uint8Array([9, 1, 2, 3, 9]);
    const result = toStandaloneBuffer(backing.subarray(1, 4));
    expect([...new Uint8Array(result)]).toEqual([1, 2, 3]);
  });

  it('handles empty bodies', () => {
    expect(toStandaloneBuffer(null).byteLength).toBe(0);
  });
});
