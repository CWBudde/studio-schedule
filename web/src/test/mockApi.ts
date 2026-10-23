import { vi } from 'vitest';
import { api } from '@/api/client';

type Fixtures = Partial<Record<string, { status?: number; body: unknown }>>;

/**
 * Replaces `api.GET` with a stub answering from fixtures keyed by path.
 * Unknown paths answer 404.
 */
export function mockApiGet(fixtures: Fixtures) {
  return vi.spyOn(api, 'GET').mockImplementation(((path: string) => {
    const fixture = fixtures[path];
    const status = fixture?.status ?? (fixture ? 200 : 404);
    const ok = status >= 200 && status < 300;
    return Promise.resolve({
      data: ok ? fixture?.body : undefined,
      error: ok ? undefined : (fixture?.body ?? { message: 'not found' }),
      response: new Response(null, { status }),
    });
  }) as unknown as typeof api.GET);
}

export const healthOk = { status: 'ok', version: '0.1.0-test' };
export const publicSettings = {
  schoolName: 'Example Dance Studio',
  timezone: 'Europe/Berlin',
  defaultLocale: 'en',
};
