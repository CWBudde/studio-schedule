import { CookieJar, pathOnly } from './cookieJar';
import type {
  GoBridgeAPI,
  GoBridgeResponse,
  HeaderPairs,
  WorkerRequestMessage,
  WorkerToMainMessage,
} from './protocol';

/**
 * Worker-side request handling, kept free of worker globals so it can be unit
 * tested: applies the cookie jar, calls into Go and converts the result into
 * a transferable message. `onCookies` is called after a response that set a
 * cookie, so the session is saved with the data (data changes are reported
 * by Go itself, see worker.ts).
 */
export function createRequestHandler(
  api: GoBridgeAPI,
  jar: CookieJar = new CookieJar(),
  onCookies: () => void = () => undefined,
) {
  return async function handle(msg: WorkerRequestMessage): Promise<WorkerToMainMessage> {
    const headers: HeaderPairs = msg.headers.filter(([name]) => name.toLowerCase() !== 'cookie');
    const cookie = jar.getCookieHeader(msg.url);
    if (cookie !== null) headers.push(['cookie', cookie]);

    try {
      const res = await api.fetch({
        method: msg.method.toUpperCase(),
        url: msg.url,
        headers,
        body: msg.body ? new Uint8Array(msg.body) : null,
      });

      const responseHeaders: HeaderPairs = [];
      let cookies = false;
      for (const [name, value] of normalizeHeaders(res.headers)) {
        if (name.toLowerCase() === 'set-cookie') {
          jar.setCookie(value, pathOnly(msg.url));
          cookies = true;
        } else {
          responseHeaders.push([name, value]);
        }
      }
      if (cookies) onCookies();

      return {
        type: 'response',
        id: msg.id,
        status: res.status,
        statusText: '',
        headers: responseHeaders,
        body: toStandaloneBuffer(res.body),
      };
    } catch (err) {
      return { type: 'error', id: msg.id, message: errorMessage(err) };
    }
  };
}

function normalizeHeaders(headers: GoBridgeResponse['headers']): HeaderPairs {
  if (!headers) return [];
  if (Array.isArray(headers)) {
    return headers.map(([name, value]) => [String(name), String(value)]);
  }
  return Object.entries(headers).map(([name, value]) => [name, String(value)]);
}

/**
 * Returns an ArrayBuffer that holds exactly the body bytes, so it can be
 * transferred (zero-copy) to the main thread.
 */
export function toStandaloneBuffer(body: Uint8Array | null | undefined): ArrayBuffer {
  if (!body || body.byteLength === 0) return new ArrayBuffer(0);
  const { buffer } = body;
  if (
    buffer instanceof ArrayBuffer &&
    body.byteOffset === 0 &&
    body.byteLength === buffer.byteLength
  ) {
    return buffer;
  }
  const copy = new Uint8Array(body.byteLength);
  copy.set(body);
  return copy.buffer;
}

export function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
