import { describe, expect, it } from 'vitest';
import { appPathOf, splitLinks } from './appLinks';

describe('appPathOf', () => {
  const here = { origin: 'https://demo.example.org', pathname: '/studio-schedule/' };

  it('maps a hash-routed link of the app to its route', () => {
    expect(
      appPathOf('https://demo.example.org/studio-schedule/#/confirm?token=a%2Bb', here, 'hash'),
    ).toBe('/confirm?token=a%2Bb');
  });

  it('leaves hash-routed links of other pages and sites alone', () => {
    expect(appPathOf('https://demo.example.org/other/#/confirm', here, 'hash')).toBeNull();
    expect(appPathOf('https://example.com/studio-schedule/#/confirm', here, 'hash')).toBeNull();
    expect(appPathOf('https://demo.example.org/studio-schedule/', here, 'hash')).toBeNull();
    expect(appPathOf('https://demo.example.org/studio-schedule/#main', here, 'hash')).toBeNull();
  });

  it('maps a same-origin link to its path with browser history', () => {
    expect(appPathOf('https://demo.example.org/confirm?token=x', here, 'browser')).toBe(
      '/confirm?token=x',
    );
    expect(appPathOf('https://example.com/confirm?token=x', here, 'browser')).toBeNull();
  });

  it('leaves links that are not http(s) alone', () => {
    expect(appPathOf('mailto:office@example.org', here, 'browser')).toBeNull();
    expect(appPathOf('not a url', here, 'browser')).toBeNull();
  });
});

describe('splitLinks', () => {
  it('splits text into text and http(s) links', () => {
    expect(splitLinks('Open https://example.org/a?b=1 now')).toEqual([
      { text: 'Open ' },
      { url: 'https://example.org/a?b=1' },
      { text: ' now' },
    ]);
  });

  it('leaves trailing punctuation out of a link', () => {
    expect(splitLinks('See http://example.org/x. Or (https://example.com/y)!')).toEqual([
      { text: 'See ' },
      { url: 'http://example.org/x' },
      { text: '. Or (' },
      { url: 'https://example.com/y' },
      { text: ')!' },
    ]);
  });

  it('returns plain text unchanged', () => {
    expect(splitLinks('No links here.')).toEqual([{ text: 'No links here.' }]);
    expect(splitLinks('')).toEqual([]);
  });
});
