import { describe, expect, it, vi } from 'vitest';
import { downloadCalendarFeed, subscriptionUrl, webcalUrl } from './calendarFeedLinks';

const created = {
  feed: { id: 'f', scope: 'bookings' as const, createdAt: '2026-10-25T09:00:00Z' },
  token: 'tok',
  path: '/ical/tok.ics',
};

describe('calendar feed links', () => {
  it('subscribes to the URL of the API, else to the path below the page origin', () => {
    expect(subscriptionUrl({ ...created, url: 'https://studio.example.org/ical/tok.ics' })).toBe(
      'https://studio.example.org/ical/tok.ics',
    );
    expect(subscriptionUrl(created)).toBe(`${window.location.origin}/ical/tok.ics`);
  });

  it('downloads a feed from the configured path even when it starts with /api', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    try {
      await downloadCalendarFeed({ ...created, path: '/api/studio/ical/tok.ics' });
      expect(open).toHaveBeenCalledWith(
        `${window.location.origin}/api/studio/ical/tok.ics?download=true`,
        '_blank',
        'noopener',
      );
    } finally {
      open.mockRestore();
    }
  });

  it('turns http(s) into webcal', () => {
    expect(webcalUrl('https://studio.example.org/ical/tok.ics')).toBe(
      'webcal://studio.example.org/ical/tok.ics',
    );
    expect(webcalUrl('http://localhost:8080/ical/tok.ics')).toBe(
      'webcal://localhost:8080/ical/tok.ics',
    );
  });
});
