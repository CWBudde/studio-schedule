import { vi } from 'vitest';
import { api } from '@/api/client';
import type { LessonDetail, LessonSummary, Schedule } from '@/api/client';

type Answer = { status?: number; body: unknown };

/** Request options as openapi-fetch receives them, e.g. { params: { query } }. */
export type MockRequest = {
  params?: { query?: Record<string, unknown>; path?: Record<string, string> };
};

/** A fixed answer, or a function of the request (to vary by query or path params). */
type Fixture = Answer | ((init: MockRequest) => Answer);
type Fixtures = Partial<Record<string, Fixture>>;

/**
 * Replaces `api.GET` with a stub answering from fixtures keyed by path
 * template (e.g. '/api/v1/lessons/{lessonId}'). Unknown paths answer 404.
 */
export function mockApiGet(fixtures: Fixtures) {
  return vi.spyOn(api, 'GET').mockImplementation(((path: string, init?: MockRequest) => {
    const fixture = fixtures[path];
    const answer = typeof fixture === 'function' ? fixture(init ?? {}) : fixture;
    const status = answer?.status ?? (answer ? 200 : 404);
    const ok = status >= 200 && status < 300;
    return Promise.resolve({
      data: ok ? answer?.body : undefined,
      error: ok ? undefined : (answer?.body ?? { message: 'not found' }),
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

// Fictional catalogue and schedule. IDs are valid UUIDv7 strings.
const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

export const ids = {
  locationNorth: id(1),
  locationSouth: id(2),
  roomA: id(11),
  roomB: id(12),
  staffAlex: id(21),
  staffSam: id(22),
  staffRiley: id(23),
  ballet: id(31),
  contemporary: id(32),
  jazz: id(33),
  lessonBallet: id(101),
  lessonContemporary: id(102),
  lessonWorkshop: id(103),
  classBallet: id(201),
  classContemporary: id(202),
};

export const locations = [
  { id: ids.locationNorth, name: 'North Studio', address: '1 Example Street, Sampletown' },
  { id: ids.locationSouth, name: 'South Studio' },
];

export const staff = [
  { id: ids.staffAlex, displayName: 'Alex Example' },
  { id: ids.staffRiley, displayName: 'Riley Sample' },
  { id: ids.staffSam, displayName: 'Sam Pianist' },
];

export const courseTypes = [
  { id: ids.ballet, name: 'Ballet', description: 'Classical ballet technique.' },
  { id: ids.contemporary, name: 'Contemporary' },
  { id: ids.jazz, name: 'Jazz' },
];

/** Regular ballet class, Monday 2026-10-26 18:00–19:30 in Berlin, with an accompanist. */
export const lessonBallet: LessonSummary = {
  id: ids.lessonBallet,
  classId: ids.classBallet,
  courseType: { id: ids.ballet, name: 'Ballet' },
  room: { id: ids.roomA, name: 'Room A' },
  location: { id: ids.locationNorth, name: 'North Studio' },
  instructor: { id: ids.staffAlex, displayName: 'Alex Example' },
  accompanist: { id: ids.staffSam, displayName: 'Sam Pianist' },
  startsAt: '2026-10-26T17:00:00Z',
  endsAt: '2026-10-26T18:30:00Z',
  status: 'scheduled',
  kind: 'regular',
  level: 'beginner',
  capacity: 12,
};

/** Canceled contemporary class, Tuesday 2026-10-27 17:00–18:00 in Berlin. */
export const lessonContemporary: LessonSummary = {
  id: ids.lessonContemporary,
  classId: ids.classContemporary,
  courseType: { id: ids.contemporary, name: 'Contemporary' },
  room: { id: ids.roomB, name: 'Room B' },
  location: { id: ids.locationSouth, name: 'South Studio' },
  instructor: { id: ids.staffRiley, displayName: 'Riley Sample' },
  startsAt: '2026-10-27T16:00:00Z',
  endsAt: '2026-10-27T17:00:00Z',
  status: 'canceled',
  kind: 'regular',
  level: 'intermediate',
  capacity: 10,
};

/** One-off jazz workshop, Saturday 2026-11-07 14:00–17:00 in Berlin (no class, no level). */
export const lessonWorkshop: LessonSummary = {
  id: ids.lessonWorkshop,
  courseType: { id: ids.jazz, name: 'Jazz' },
  room: { id: ids.roomA, name: 'Room A' },
  location: { id: ids.locationNorth, name: 'North Studio' },
  instructor: { id: ids.staffRiley, displayName: 'Riley Sample' },
  startsAt: '2026-11-07T13:00:00Z',
  endsAt: '2026-11-07T16:00:00Z',
  status: 'scheduled',
  kind: 'workshop',
  capacity: 20,
};

/** Schedule of 2026-10-26 (Monday) to 2026-11-09, as the API returns it on Friday 2026-10-23. */
export const schedule: Schedule = {
  timezone: 'Europe/Berlin',
  now: '2026-10-23T10:00:00Z',
  from: '2026-10-26',
  to: '2026-11-09',
  releasedUntil: '2026-11-01T23:00:00Z',
  nextReleaseAt: '2026-10-25T17:00:00Z',
  lessons: [lessonBallet, lessonContemporary, lessonWorkshop],
};

export const lessonBalletDetail: LessonDetail = {
  ...lessonBallet,
  courseTypeDescription: 'Classical ballet technique.',
  locationAddress: '1 Example Street, Sampletown',
};

/** Catalogue fixtures for the filter selects. */
export const catalogFixtures = {
  '/api/v1/locations': { body: { items: locations } },
  '/api/v1/staff': { body: { items: staff } },
  '/api/v1/course-types': { body: { items: courseTypes } },
};
