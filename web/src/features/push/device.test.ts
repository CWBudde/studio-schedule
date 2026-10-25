import { afterEach, describe, expect, it } from 'vitest';
import { notificationOptions, parsePushPayload } from '@/lib/pushPayload';
import { base64UrlToBytes, bytesToBase64Url, demoDevice, deviceLabel } from './device';

afterEach(() => localStorage.clear());

describe('deviceLabel', () => {
  it('names the browser and the system', () => {
    expect(
      deviceLabel('Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0'),
    ).toBe('Firefox · Linux');
    expect(
      deviceLabel(
        'Mozilla/5.0 (Linux; Android 15; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Mobile Safari/537.36',
      ),
    ).toBe('Chrome · Android');
    expect(
      deviceLabel(
        'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',
      ),
    ).toBe('Safari · iOS');
    expect(
      deviceLabel(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 Edg/140.0',
      ),
    ).toBe('Edge · Windows');
    expect(deviceLabel('curl/8')).toBe('Browser');
  });
});

describe('base64url', () => {
  it('round-trips bytes without padding', () => {
    const bytes = new Uint8Array([0xfb, 0xff, 0x01, 0x02]);
    const s = bytesToBase64Url(bytes);
    expect(s).toBe('-_8BAg');
    expect([...base64UrlToBytes(s)]).toEqual([...bytes]);
  });
});

describe('the demo device', () => {
  it('makes up a subscription with a real P-256 key and keeps it', async () => {
    const device = demoDevice();
    expect(await device.current()).toBeNull();

    // The demo shows its notifications through the service worker.
    Object.defineProperty(navigator, 'serviceWorker', {
      configurable: true,
      value: { getRegistration: () => Promise.resolve({}) },
    });
    try {
      const sub = await device.subscribe(undefined);
      expect(sub.endpoint).toMatch(/^https:\/\/push\.invalid\/demo\/[0-9a-f-]{36}$/);
      expect(base64UrlToBytes(sub.keys.p256dh)).toHaveLength(65);
      expect(base64UrlToBytes(sub.keys.p256dh)[0]).toBe(4);
      expect(base64UrlToBytes(sub.keys.auth)).toHaveLength(16);
      expect(await device.current()).toEqual(sub);

      expect(await device.unsubscribe()).toEqual(sub);
      expect(await device.current()).toBeNull();
    } finally {
      Reflect.deleteProperty(navigator, 'serviceWorker');
    }
  });
});

describe('the push payload', () => {
  it('reads what the server queues and shows it with the app icon', () => {
    const p = parsePushPayload(
      '{"title":"A place is free: Tap","body":"Tue, 3 Nov","url":"https://x/#/waitlist/accept?token=t","tag":"waitlist-invite-1","lang":"en"}',
    );
    expect(p).toEqual({
      title: 'A place is free: Tap',
      body: 'Tue, 3 Nov',
      url: 'https://x/#/waitlist/accept?token=t',
      tag: 'waitlist-invite-1',
      lang: 'en',
    });
    expect(notificationOptions(p!, '/icons/icon-192.png')).toEqual({
      body: 'Tue, 3 Nov',
      icon: '/icons/icon-192.png',
      badge: '/icons/icon-192.png',
      lang: 'en',
      tag: 'waitlist-invite-1',
      data: { url: 'https://x/#/waitlist/accept?token=t' },
    });
  });

  it('refuses anything without a title', () => {
    expect(parsePushPayload('not json')).toBeNull();
    expect(parsePushPayload({ body: 'x' })).toBeNull();
    expect(parsePushPayload(null)).toBeNull();
  });
});
