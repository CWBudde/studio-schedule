import { describe, expect, it } from 'vitest';
import { bypassesServiceWorker } from './swRoutes';

describe('the service worker leaves to the server', () => {
  for (const base of ['/', '/studio-schedule/']) {
    it(`the API, feeds, manifest, probes and metrics under ${base}`, () => {
      for (const path of [
        'api',
        'api/v1/health',
        'api/v1/docs?x=1',
        'ical/token.ics',
        'manifest.webmanifest',
        'healthz',
        'readyz',
        'readyz?verbose=1',
        'metrics',
      ]) {
        expect(bypassesServiceWorker(base, base + path), path).toBe(true);
      }
    });

    it(`but answers the app's routes under ${base}`, () => {
      for (const path of [
        '',
        'schedule',
        'my-lessons?view=list',
        'account',
        'apiary',
        'healthzone',
      ]) {
        expect(bypassesServiceWorker(base, base + path), path).toBe(false);
      }
    });
  }

  it('only below its own base', () => {
    expect(bypassesServiceWorker('/studio-schedule/', '/api/v1/health')).toBe(false);
  });
});
