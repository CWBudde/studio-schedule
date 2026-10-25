import { describe, expect, it } from 'vitest';
import { subscriptionUrl, webcalUrl } from './calendarFeedLinks';

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

  it('turns http(s) into webcal', () => {
    expect(webcalUrl('https://studio.example.org/ical/tok.ics')).toBe(
      'webcal://studio.example.org/ical/tok.ics',
    );
    expect(webcalUrl('http://localhost:8080/ical/tok.ics')).toBe(
      'webcal://localhost:8080/ical/tok.ics',
    );
  });
});
