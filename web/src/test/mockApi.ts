import { vi } from 'vitest';
import { api } from '@/api/client';
import type { components } from '@/api/schema';
import type {
  AccountEntry,
  AuditEventEntry,
  DemoLogin,
  LessonDetail,
  MailboxMail,
  Me,
  Schedule,
  ScheduleLesson,
} from '@/api/client';

type PersonMatch = components['schemas']['PersonMatch'];
type StaffEntry = components['schemas']['StaffEntry'];

type Answer = { status?: number; body?: unknown; headers?: Record<string, string> };

/** Request options as openapi-fetch receives them, e.g. { params: { query } }. */
export type MockRequest = {
  params?: { query?: Record<string, unknown>; path?: Record<string, string> };
  body?: unknown;
};

/** A fixed answer, or a function of the request (to vary by query or path params). */
type Fixture = Answer | ((init: MockRequest) => Answer);
type Fixtures = Partial<Record<string, Fixture>>;

function answerFrom(fixtures: Fixtures, path: string, init?: MockRequest) {
  const fixture = fixtures[path];
  const answer = typeof fixture === 'function' ? fixture(init ?? {}) : fixture;
  const status = answer?.status ?? (answer ? 200 : 404);
  const ok = status >= 200 && status < 300;
  // Like openapi-fetch: an answer without a body has no data.
  const hasBody = answer?.body !== undefined && status !== 204;
  return Promise.resolve({
    data: ok && hasBody ? answer?.body : undefined,
    error: ok ? undefined : (answer?.body ?? { title: 'Not Found', status }),
    response: new Response(null, { status, headers: answer?.headers }),
  });
}

/**
 * Replaces `api.GET` with a stub answering from fixtures keyed by path
 * template (e.g. '/api/v1/lessons/{lessonId}'). Unknown paths answer 404.
 */
export function mockApiGet(fixtures: Fixtures) {
  return vi
    .spyOn(api, 'GET')
    .mockImplementation(((path: string, init?: MockRequest) =>
      answerFrom(fixtures, path, init)) as unknown as typeof api.GET);
}

/**
 * Replaces `api.POST` like mockApiGet; a fixture function sees the request
 * body in `init.body`. An answer without `body` (e.g. `{ status: 202 }`) has
 * no data, as with openapi-fetch. Unknown paths answer 404.
 */
export function mockApiPost(fixtures: Fixtures) {
  return vi
    .spyOn(api, 'POST')
    .mockImplementation(((path: string, init?: MockRequest) =>
      answerFrom(fixtures, path, init)) as unknown as typeof api.POST);
}

/** Replaces `api.PUT` like mockApiPost. */
export function mockApiPut(fixtures: Fixtures) {
  return vi
    .spyOn(api, 'PUT')
    .mockImplementation(((path: string, init?: MockRequest) =>
      answerFrom(fixtures, path, init)) as unknown as typeof api.PUT);
}

/** Replaces `api.DELETE` like mockApiPost. */
export function mockApiDelete(fixtures: Fixtures) {
  return vi
    .spyOn(api, 'DELETE')
    .mockImplementation(((path: string, init?: MockRequest) =>
      answerFrom(fixtures, path, init)) as unknown as typeof api.DELETE);
}

export const healthOk = { status: 'ok', version: '0.1.0-test' };
export const publicSettings = {
  schoolName: 'Example Dance Studio',
  timezone: 'Europe/Berlin',
  defaultLocale: 'en',
  currency: 'EUR',
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
  staffQuinn: id(24),
  ballet: id(31),
  contemporary: id(32),
  jazz: id(33),
  lessonBallet: id(101),
  lessonContemporary: id(102),
  lessonWorkshop: id(103),
  classBallet: id(201),
  classContemporary: id(202),
  classJazz: id(203),
  classSpring: id(204),
  classRetired: id(205),
  termPast: id(501),
  termAutumn: id(502),
  termSpring: id(503),
  personAlex: id(341),
  personMia: id(342),
  personNoah: id(343),
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

/** Fictional persons as GET /admin/persons finds them (ordered by family and given name). */
export const persons: PersonMatch[] = [
  {
    id: ids.personAlex,
    givenName: 'Alex',
    familyName: 'Example',
    email: 'alex.example@example.com',
  },
  { id: ids.personMia, givenName: 'Mia', familyName: 'Sample', email: 'mia.sample@example.org' },
  { id: ids.personNoah, givenName: 'Noah', familyName: 'Sample' },
];

/**
 * All staff as GET /admin/staff lists them for the office (ordered by
 * display name): Alex linked to a person, a former instructor, and two
 * without a person.
 */
export const staffEntries: StaffEntry[] = [
  {
    id: ids.staffAlex,
    displayName: 'Alex Example',
    kind: 'instructor',
    person: persons[0],
    active: true,
  },
  { id: ids.staffQuinn, displayName: 'Quinn Former', kind: 'instructor', active: false },
  { id: ids.staffRiley, displayName: 'Riley Sample', kind: 'instructor', active: true },
  { id: ids.staffSam, displayName: 'Sam Pianist', kind: 'accompanist', active: true },
];

export const rooms = [
  { id: ids.roomA, locationId: ids.locationNorth, name: 'Room A', capacity: 16 },
  { id: ids.roomB, locationId: ids.locationSouth, name: 'Room B', capacity: 12 },
];

export const courseTypes = [
  { id: ids.ballet, name: 'Ballet', description: 'Classical ballet technique.' },
  { id: ids.contemporary, name: 'Contemporary' },
  { id: ids.jazz, name: 'Jazz' },
];

/**
 * Regular ballet class, Monday 2026-10-26 18:00–19:30 in Berlin, with an
 * accompanist. Like every fixture lesson, booking closes an hour before the
 * start, and a cancellation from 24 hours before the start on is late.
 */
export const lessonBallet: ScheduleLesson = {
  id: ids.lessonBallet,
  classId: ids.classBallet,
  courseType: { id: ids.ballet, name: 'Ballet' },
  room: { id: ids.roomA, name: 'Room A' },
  location: { id: ids.locationNorth, name: 'North Studio' },
  instructor: { id: ids.staffAlex, displayName: 'Alex Example' },
  accompanist: { id: ids.staffSam, displayName: 'Sam Pianist' },
  startsAt: '2026-10-26T17:00:00Z',
  endsAt: '2026-10-26T18:30:00Z',
  bookingClosesAt: '2026-10-26T16:00:00Z',
  lateCancelFrom: '2026-10-25T17:00:00Z',
  status: 'scheduled',
  kind: 'regular',
  level: 'beginner',
  capacity: 12,
  visibility: 'public',
  freePlaces: 12,
};

/** Canceled contemporary class, Tuesday 2026-10-27 17:00–18:00 in Berlin. */
export const lessonContemporary: ScheduleLesson = {
  id: ids.lessonContemporary,
  classId: ids.classContemporary,
  courseType: { id: ids.contemporary, name: 'Contemporary' },
  room: { id: ids.roomB, name: 'Room B' },
  location: { id: ids.locationSouth, name: 'South Studio' },
  instructor: { id: ids.staffRiley, displayName: 'Riley Sample' },
  startsAt: '2026-10-27T16:00:00Z',
  endsAt: '2026-10-27T17:00:00Z',
  bookingClosesAt: '2026-10-27T15:00:00Z',
  status: 'canceled',
  kind: 'regular',
  level: 'intermediate',
  capacity: 10,
  visibility: 'public',
  freePlaces: 10,
};

/** One-off jazz workshop, Saturday 2026-11-07 14:00–17:00 in Berlin (no class, no level). */
export const lessonWorkshop: ScheduleLesson = {
  id: ids.lessonWorkshop,
  courseType: { id: ids.jazz, name: 'Jazz' },
  room: { id: ids.roomA, name: 'Room A' },
  location: { id: ids.locationNorth, name: 'North Studio' },
  instructor: { id: ids.staffRiley, displayName: 'Riley Sample' },
  startsAt: '2026-11-07T13:00:00Z',
  endsAt: '2026-11-07T16:00:00Z',
  bookingClosesAt: '2026-11-07T12:00:00Z',
  lateCancelFrom: '2026-11-06T13:00:00Z',
  status: 'scheduled',
  kind: 'workshop',
  capacity: 20,
  visibility: 'public',
  freePlaces: 20,
};

/**
 * Schedule of 2026-10-26 (Monday) to 2026-11-09, as the API returns it to a
 * guest on Friday 2026-10-23.
 */
export const schedule: Schedule = {
  timezone: 'Europe/Berlin',
  now: '2026-10-23T10:00:00Z',
  from: '2026-10-26',
  to: '2026-11-09',
  releasedUntil: '2026-11-01T23:00:00Z',
  visibleUntil: '2026-11-01T23:00:00Z',
  nextReleaseAt: '2026-10-25T17:00:00Z',
  lessons: [lessonBallet, lessonContemporary, lessonWorkshop],
};

export const lessonBalletDetail: LessonDetail = {
  ...lessonBallet,
  courseTypeDescription: 'Classical ballet technique.',
  locationAddress: '1 Example Street, Sampletown',
};

/**
 * The ballet lesson as the office sees it: with the admin details, two
 * active bookings and still following its class.
 */
export const lessonBalletOffice: LessonDetail = {
  ...lessonBalletDetail,
  freePlaces: 10,
  admin: { detached: false, activeBookings: 2 },
};

/** The one-off jazz workshop as the office sees it: nobody has booked it. */
export const lessonWorkshopOffice: LessonDetail = {
  ...lessonWorkshop,
  admin: { detached: false, activeBookings: 0 },
};

/**
 * The canceled contemporary lesson as the office sees it: canceled by the
 * office worker on Friday 2026-10-23 12:30 in Berlin, with a note, and
 * detached from its class.
 */
export const lessonContemporaryOffice: LessonDetail = {
  ...lessonContemporary,
  freePlaces: 9,
  admin: {
    detached: true,
    activeBookings: 1,
    cancelNote: 'The instructor is ill.',
    canceledAt: '2026-10-23T10:30:00Z',
    canceledBy: { id: '0190a000-0000-7000-8000-000000000321', name: 'Otto Office' },
  },
};

/** Catalogue fixtures for the filter selects. */
export const catalogFixtures = {
  '/api/v1/locations': { body: { items: locations } },
  '/api/v1/rooms': { body: { items: rooms } },
  '/api/v1/staff': { body: { items: staff } },
  '/api/v1/course-types': { body: { items: courseTypes } },
};

type Term = components['schemas']['Term'];
type Class = components['schemas']['Class'];

/**
 * Fictional terms, ordered by start date as the API returns them. Friday
 * 2026-10-23 (the fixtures' today) falls into the autumn term.
 */
export const terms: Term[] = [
  { id: ids.termPast, name: 'School Year 2025/26', startsOn: '2025-09-01', endsOn: '2026-07-15' },
  { id: ids.termAutumn, name: 'Autumn 2026', startsOn: '2026-08-31', endsOn: '2027-01-29' },
  { id: ids.termSpring, name: 'Spring 2027', startsOn: '2027-02-01', endsOn: '2027-07-16' },
];

/**
 * Weekly classes of the autumn term in the API's order (Sunday first): a
 * Sunday jazz class, Monday ballet with an accompanist, Tuesday contemporary.
 */
export const autumnClasses: Class[] = [
  {
    id: ids.classJazz,
    termId: ids.termAutumn,
    courseTypeId: ids.jazz,
    roomId: ids.roomB,
    instructor: { id: ids.staffRiley, displayName: 'Riley Sample' },
    level: 'open',
    weekday: 0,
    startTime: '11:00',
    durationMinutes: 90,
    capacity: 12,
  },
  {
    id: ids.classBallet,
    termId: ids.termAutumn,
    courseTypeId: ids.ballet,
    roomId: ids.roomA,
    instructor: { id: ids.staffAlex, displayName: 'Alex Example' },
    accompanist: { id: ids.staffSam, displayName: 'Sam Pianist' },
    level: 'beginner',
    weekday: 1,
    startTime: '18:00',
    durationMinutes: 90,
    capacity: 12,
  },
  {
    id: ids.classContemporary,
    termId: ids.termAutumn,
    courseTypeId: ids.contemporary,
    roomId: ids.roomB,
    instructor: { id: ids.staffRiley, displayName: 'Riley Sample' },
    level: 'intermediate',
    weekday: 2,
    startTime: '17:00',
    durationMinutes: 60,
    capacity: 10,
  },
];

type ClassEntry = components['schemas']['ClassEntry'];

/**
 * The classes of the autumn term as the office lists them
 * (GET /admin/terms/{termId}/classes): the public ones and a retired
 * Saturday jazz class, in the API's order (Sunday first).
 */
export const autumnClassEntries: ClassEntry[] = [
  ...autumnClasses.map((c) => ({ ...c, retired: false })),
  {
    id: ids.classRetired,
    termId: ids.termAutumn,
    courseTypeId: ids.jazz,
    roomId: ids.roomA,
    instructor: { id: ids.staffQuinn, displayName: 'Quinn Former' },
    level: 'beginner',
    weekday: 6,
    startTime: '10:00',
    durationMinutes: 60,
    capacity: 16,
    retired: true,
  },
];

/** The one weekly class of the spring term: Thursday ballet. */
export const springClasses: Class[] = [
  {
    id: ids.classSpring,
    termId: ids.termSpring,
    courseTypeId: ids.ballet,
    roomId: ids.roomA,
    instructor: { id: ids.staffAlex, displayName: 'Alex Example' },
    level: 'advanced',
    weekday: 4,
    startTime: '19:15',
    durationMinutes: 75,
    capacity: 14,
  },
];

type ClassMember = components['schemas']['ClassMember'];
type PersonDetail = components['schemas']['PersonDetail'];

/**
 * The members of the Monday ballet class as the office lists them
 * (GET /admin/classes/{classId}/members, by family and given name): Mia
 * since the start of the autumn term, Noah since an unknown date.
 */
export const balletMembers: ClassMember[] = [
  {
    person: {
      id: ids.personMia,
      givenName: 'Mia',
      familyName: 'Sample',
      email: 'mia.sample@example.org',
    },
    since: '2026-08-31',
  },
  { person: { id: ids.personNoah, givenName: 'Noah', familyName: 'Sample' } },
];

/**
 * Persons as the office sees them in detail (GET /admin/persons/{personId}):
 * Mia, a member with an account and her son Noah; Noah, a child in the
 * Monday ballet class with Mia as his guardian; Alex, an instructor's person
 * record with an account and a staff link.
 */
export const personDetails: Record<string, PersonDetail> = {
  [ids.personMia]: {
    id: ids.personMia,
    givenName: 'Mia',
    familyName: 'Sample',
    email: 'mia.sample@example.org',
    phone: '+49 000 0000201',
    notes: 'Prefers to be called in the evening.',
    guardians: [],
    dependents: [persons[2]!],
    account: {
      id: id(351),
      email: 'mia.sample@example.org',
      role: 'member',
      status: 'active',
    },
    staff: [],
    classes: [],
    merged: [],
  },
  [ids.personNoah]: {
    id: ids.personNoah,
    givenName: 'Noah',
    familyName: 'Sample',
    birthDate: '2015-04-09',
    guardians: [persons[1]!],
    dependents: [],
    staff: [],
    classes: [
      {
        classId: ids.classBallet,
        termId: ids.termAutumn,
        termName: 'Autumn 2026',
        courseTypeName: 'Ballet',
        weekday: 1,
        startTime: '18:00',
        retired: false,
      },
    ],
    merged: [],
  },
  [ids.personAlex]: {
    id: ids.personAlex,
    givenName: 'Alex',
    familyName: 'Example',
    email: 'alex.example@example.com',
    guardians: [],
    dependents: [],
    account: {
      id: id(352),
      email: 'alex.example@example.com',
      role: 'instructor',
      status: 'pending',
    },
    staff: [{ id: ids.staffAlex, displayName: 'Alex Example', kind: 'instructor', active: true }],
    classes: [
      {
        classId: ids.classRetired,
        termId: ids.termAutumn,
        termName: 'Autumn 2026',
        courseTypeName: 'Jazz',
        weekday: 6,
        startTime: '10:00',
        retired: true,
        since: '2026-08-31',
      },
    ],
    merged: [],
  },
};

/** The fictional member signed in in auth tests. */
export const meMember: Me = {
  accountId: id(301),
  personId: id(302),
  email: 'nora.example@example.org',
  role: 'member',
  givenName: 'Nora',
  familyName: 'Example',
  teaches: false,
};

/** A fictional admin, as GET /auth/me returns them. */
export const meAdmin: Me = {
  accountId: id(311),
  personId: id(312),
  email: 'ada.admin@example.com',
  role: 'admin',
  givenName: 'Ada',
  familyName: 'Admin',
  teaches: false,
};

/** A fictional office worker, as GET /auth/me returns them. */
export const meOffice: Me = {
  accountId: id(321),
  personId: id(322),
  email: 'otto.office@example.com',
  role: 'office',
  givenName: 'Otto',
  familyName: 'Office',
  teaches: false,
};

/** The member as the admin sees them while signed in as them ("login as"). */
export const meImpersonated: Me = {
  ...meMember,
  impersonator: {
    accountId: meAdmin.accountId,
    email: meAdmin.email,
    givenName: meAdmin.givenName,
    familyName: meAdmin.familyName,
  },
};

type Family = components['schemas']['Family'];

/**
 * The family of the member as GET /family returns it: two children, the
 * first with another guardian, the second with Nora as the only one.
 */
export const family: Family = {
  me: {
    id: meMember.personId,
    givenName: 'Nora',
    familyName: 'Example',
    phone: '+49 000 0000999',
    birthDate: '1988-06-15',
  },
  dependents: [
    {
      id: id(331),
      givenName: 'Lina',
      familyName: 'Example',
      birthDate: '2017-03-14',
      guardians: [{ givenName: 'Paul', familyName: 'Example' }],
      managed: false,
    },
    {
      id: id(332),
      givenName: 'Max',
      familyName: 'Example',
      guardians: [],
      managed: false,
    },
  ],
};

/**
 * A grown-up child of the member with an account of their own, which the
 * office created: a managed dependent, read-only for the guardians.
 */
export const managedDependent: Family['dependents'][number] = {
  id: id(333),
  givenName: 'Jule',
  familyName: 'Example',
  birthDate: '2008-09-01',
  guardians: [{ givenName: 'Paul', familyName: 'Example' }],
  managed: true,
};

/**
 * Fictional accounts as GET /admin/accounts lists them (ordered by email):
 * the admin, a member, an instructor, an unconfirmed and a disabled member.
 */
export const accounts: AccountEntry[] = [
  {
    id: meAdmin.accountId,
    email: meAdmin.email,
    role: 'admin',
    givenName: 'Ada',
    familyName: 'Admin',
    confirmed: true,
    disabled: false,
  },
  {
    id: id(321),
    email: 'alex.example@example.com',
    role: 'instructor',
    givenName: 'Alex',
    familyName: 'Example',
    confirmed: true,
    disabled: false,
  },
  {
    id: id(322),
    email: 'nina.new@example.org',
    role: 'member',
    givenName: 'Nina',
    familyName: 'New',
    confirmed: false,
    disabled: false,
  },
  {
    id: meMember.accountId,
    email: meMember.email,
    role: 'member',
    givenName: 'Nora',
    familyName: 'Example',
    confirmed: true,
    disabled: false,
  },
  {
    id: id(323),
    email: 'olaf.old@example.org',
    role: 'member',
    givenName: 'Olaf',
    familyName: 'Old',
    confirmed: true,
    disabled: true,
  },
];

/**
 * Fictional audit events as GET /admin/audit lists them (newest first): the
 * admin signed in as Nora Example and returned; an action this frontend does
 * not know yet.
 */
export const auditEvents: AuditEventEntry[] = [
  {
    id: id(343),
    at: '2026-10-25T23:30:00Z',
    action: 'account-merged',
    actor: { id: meAdmin.accountId, email: meAdmin.email, givenName: 'Ada', familyName: 'Admin' },
    target: { id: id(323), email: 'olaf.old@example.org', givenName: 'Olaf', familyName: 'Old' },
  },
  {
    id: id(342),
    at: '2026-10-23T14:45:00Z',
    action: 'impersonation-ended',
    actor: { id: meAdmin.accountId, email: meAdmin.email, givenName: 'Ada', familyName: 'Admin' },
    target: {
      id: meMember.accountId,
      email: meMember.email,
      givenName: 'Nora',
      familyName: 'Example',
    },
  },
  {
    id: id(341),
    at: '2026-10-23T14:30:00Z',
    action: 'impersonation-started',
    actor: { id: meAdmin.accountId, email: meAdmin.email, givenName: 'Ada', familyName: 'Admin' },
    target: {
      id: meMember.accountId,
      email: meMember.email,
      givenName: 'Nora',
      familyName: 'Example',
    },
  },
];

/** A problem+json answer, as the API sends it for errors. */
export const problem = (
  status: number,
  detail: string,
  errors?: { location: string; message?: string }[],
) => ({
  status,
  body: { status, title: 'Error', detail, type: 'about:blank', errors },
});

/**
 * A 409 answer of a write: problem+json with a stable `reason`, and `extra`
 * fields such as the `lesson` of a `room-taken`.
 */
export const conflict = (
  reason: components['schemas']['Conflict']['reason'],
  detail = 'conflict',
  extra: Partial<components['schemas']['Conflict']> = {},
) => ({
  status: 409,
  body: { status: 409, title: 'Conflict', detail, type: 'about:blank', reason, ...extra },
});

/**
 * A 429 answer as the API's rate limits send it: problem+json with the wait
 * in the detail and, unless `header` is false, in the Retry-After header.
 */
export const tooManyRequests = (seconds: number, { header = true } = {}) => ({
  ...problem(429, `too many attempts; try again in ${seconds} seconds`),
  headers: header ? { 'Retry-After': String(seconds) } : undefined,
});

/** Fixtures of a signed-out visitor (GET /auth/me answers 401). */
export const signedOut = { '/api/v1/auth/me': problem(401, 'not signed in') };

/** Fictional demo accounts as GET /demo/logins returns them. */
export const demoLogins: DemoLogin[] = [
  {
    email: 'nora.example@example.org',
    password: 'demo-password',
    role: 'member',
    givenName: 'Nora',
    familyName: 'Example',
  },
  {
    email: 'alex.example@example.com',
    password: 'demo-password',
    role: 'instructor',
    givenName: 'Alex',
    familyName: 'Example',
  },
  {
    email: 'olli.office@example.org',
    password: 'demo-password',
    role: 'office',
    givenName: 'Olli',
    familyName: 'Office',
  },
  {
    email: 'ada.admin@example.com',
    password: 'demo-password',
    role: 'admin',
    givenName: 'Ada',
    familyName: 'Admin',
  },
];

/**
 * A confirmation mail as the demo mailbox lists it, written Friday
 * 2026-10-23 12:00 in Berlin. Its link points to the app (`appUrl`, the
 * app's root URL ending in '/').
 */
export const confirmationMail = (appUrl: string): MailboxMail => ({
  id: id(401),
  to: 'new.dancer@example.org',
  subject: 'Confirm your email address',
  text: `Hello Nina,\n\nplease confirm your email address: ${appUrl}confirm?token=tok-123.\n\nHelp: https://example.org/help\n`,
  html: '<p>Hello Nina</p>',
  template: 'confirm-email',
  createdAt: '2026-10-23T10:00:00Z',
  status: 'pending',
});

type PassType = components['schemas']['PassType'];
type Pass = components['schemas']['Pass'];
type AdminPass = components['schemas']['AdminPass'];
type PassUse = components['schemas']['PassUse'];

/**
 * The fictional pass catalogue as GET /admin/pass-types lists it (by name):
 * a 10-visit pass, a 30-day pass and a retired single visit.
 */
export const passTypes: PassType[] = [
  {
    id: id(701),
    name: '10-visit pass',
    kind: 'visits',
    visits: 10,
    validityDays: 180,
    price: 15000,
    vatRate: 1900,
    active: true,
    createdAt: '2026-09-01T07:00:00Z',
  },
  {
    id: id(702),
    name: '30-day pass',
    kind: 'time',
    validityDays: 30,
    price: 8900,
    vatRate: 1900,
    active: true,
    createdAt: '2026-09-01T07:00:00Z',
  },
  {
    id: id(703),
    name: 'Single visit',
    kind: 'visits',
    visits: 1,
    validityDays: 0,
    price: 1800,
    vatRate: 700,
    active: false,
    createdAt: '2026-09-01T07:00:00Z',
  },
];

const typeRef = (n: number) => ({ id: passTypes[n]!.id, name: passTypes[n]!.name });

/** A use of a pass: the ballet lesson on `startsAt` booked for `personName`. */
const passUse = (
  n: number,
  startsAt: string,
  personName: string,
  extra: Partial<PassUse> = {},
) => ({
  bookingId: id(760 + n),
  bookingStatus: 'attended' as const,
  late: false,
  lesson: { ...lessonBallet, startsAt, endsAt: startsAt.replace('T17:', 'T18:') },
  personName,
  redeemedAt: startsAt,
  ...extra,
});

const nora = { id: meMember.personId, name: 'Nora Example' };
const lina = { id: id(331), name: 'Lina Example' };
const max = { id: id(332), name: 'Max Example' };

/**
 * The passes of the member's family as GET /my/passes lists them on Friday
 * 2026-10-23: Nora's usable 10-visit pass, Lina's 30-day pass Nora paid,
 * Nora's used-up single visit and voided pass, and Max's expired pass the
 * school gave him.
 */
export const myPasses: Pass[] = [
  {
    id: id(711),
    passType: typeRef(0),
    kind: 'visits',
    owner: nora,
    origin: 'sale',
    pricePaid: 15000,
    soldAt: '2026-10-01T08:00:00Z',
    status: 'active',
    usable: true,
    validFrom: '2026-10-01',
    validUntil: '2027-03-29',
    visitsTotal: 10,
    visitsRemaining: 7,
    useCount: 3,
    uses: [
      passUse(1, '2026-10-19T17:00:00Z', 'Nora Example'),
      passUse(2, '2026-10-12T17:00:00Z', 'Nora Example', { bookingStatus: 'no_show' }),
      passUse(3, '2026-10-05T17:00:00Z', 'Nora Example'),
    ],
  },
  {
    id: id(712),
    passType: typeRef(1),
    kind: 'time',
    owner: lina,
    payer: nora,
    origin: 'sale',
    pricePaid: 8900,
    soldAt: '2026-10-10T08:00:00Z',
    status: 'active',
    usable: true,
    validFrom: '2026-10-10',
    validUntil: '2026-11-08',
    visitsTotal: 0,
    visitsRemaining: 0,
    useCount: 14,
    uses: [passUse(4, '2026-10-22T17:00:00Z', 'Lina Example')],
  },
  {
    id: id(713),
    passType: typeRef(2),
    kind: 'visits',
    owner: nora,
    origin: 'sale',
    pricePaid: 1800,
    soldAt: '2026-09-20T08:00:00Z',
    status: 'active',
    usable: false,
    validFrom: '2026-09-20',
    visitsTotal: 1,
    visitsRemaining: 0,
    useCount: 1,
    uses: [
      passUse(5, '2026-09-28T17:00:00Z', 'Nora Example', { bookingStatus: 'canceled', late: true }),
    ],
  },
  {
    id: id(714),
    passType: typeRef(0),
    kind: 'visits',
    owner: max,
    origin: 'assignment',
    pricePaid: 0,
    soldAt: '2026-03-01T08:00:00Z',
    status: 'active',
    usable: false,
    validFrom: '2026-03-01',
    validUntil: '2026-08-27',
    visitsTotal: 10,
    visitsRemaining: 4,
    useCount: 6,
    uses: [],
  },
  {
    id: id(715),
    passType: typeRef(2),
    kind: 'visits',
    owner: nora,
    origin: 'sale',
    pricePaid: 1800,
    soldAt: '2026-10-02T08:00:00Z',
    status: 'voided',
    voidedAt: '2026-10-03T08:00:00Z',
    usable: false,
    validFrom: '2026-10-02',
    visitsTotal: 1,
    visitsRemaining: 1,
    useCount: 0,
    uses: [],
  },
];

/** GET /my/passes of the member's family; the school uses passes on attendance by default. */
export const myPassesAnswer = (
  redemptionMode: 'booking' | 'attendance' = 'attendance',
  items = myPasses,
) => ({
  body: { items, today: '2026-10-23', timezone: 'Europe/Berlin', redemptionMode },
});

/**
 * Noah Sample's passes as the office lists them: a usable 10-visit pass his
 * guardian Mia paid, and a single visit voided with a note.
 */
export const noahPasses: AdminPass[] = [
  {
    id: id(721),
    passType: typeRef(0),
    kind: 'visits',
    owner: { id: ids.personNoah, name: 'Noah Sample' },
    payer: { id: ids.personMia, name: 'Mia Sample' },
    origin: 'sale',
    pricePaid: 14000,
    receiptId: id(731),
    soldAt: '2026-10-05T08:00:00Z',
    status: 'active',
    usable: true,
    validFrom: '2026-10-05',
    validUntil: '2027-04-02',
    visitsTotal: 10,
    visitsRemaining: 9,
    useCount: 1,
    uses: [passUse(6, '2026-10-19T17:00:00Z', 'Noah Sample')],
  },
  {
    id: id(722),
    passType: typeRef(2),
    kind: 'visits',
    owner: { id: ids.personNoah, name: 'Noah Sample' },
    origin: 'sale',
    pricePaid: 1800,
    soldAt: '2026-09-02T08:00:00Z',
    status: 'voided',
    voidedAt: '2026-09-03T08:00:00Z',
    usable: false,
    validFrom: '2026-09-02',
    visitsTotal: 1,
    visitsRemaining: 1,
    useCount: 0,
    uses: [],
    note: 'Sold to the wrong person.',
  },
];

type Receipt = components['schemas']['Receipt'];

const issuer = {
  name: 'Example Dance Studio',
  address: '1 Example Street\nSampletown',
  taxNumber: '00/000/00000',
  email: 'office@example.org',
};

/** A sale receipt of 140.00 € with 19 % VAT, paid by Mia Sample in cash. */
export const receiptSale: Receipt = {
  id: id(731),
  number: 'SD-2026-0002',
  kind: 'sale',
  issuedAt: '2026-10-05T08:00:00Z',
  issueDate: '2026-10-05',
  issuer,
  taxMode: 'standard',
  payer: { id: ids.personMia, name: 'Mia Sample' },
  recipientName: 'Mia Sample',
  recipientAddress: '2 Sample Road\nSampletown',
  paymentMethod: 'cash',
  currency: 'EUR',
  net: 11765,
  vat: 2235,
  gross: 14000,
  lines: [
    {
      position: 1,
      description: '10-visit pass for Noah Sample',
      quantity: 1,
      unitPrice: 14000,
      vatRate: 1900,
      net: 11765,
      vat: 2235,
      gross: 14000,
    },
  ],
  passId: id(721),
  cancelable: true,
};

/** A sale receipt canceled the next day, and its cancellation. */
export const receiptCanceled: Receipt = {
  ...receiptSale,
  id: id(732),
  number: 'SD-2026-0001',
  issuedAt: '2026-09-02T08:00:00Z',
  issueDate: '2026-09-02',
  recipientAddress: undefined,
  net: 1513,
  vat: 287,
  gross: 1800,
  lines: [
    {
      position: 1,
      description: 'Single visit for Noah Sample',
      quantity: 1,
      unitPrice: 1800,
      vatRate: 1900,
      net: 1513,
      vat: 287,
      gross: 1800,
    },
  ],
  passId: id(722),
  cancelable: false,
  canceledBy: { id: id(733), number: 'SD-2026-0003', issueDate: '2026-09-03' },
};

export const receiptCancellation: Receipt = {
  ...receiptCanceled,
  id: id(733),
  number: 'SD-2026-0003',
  kind: 'cancellation',
  issuedAt: '2026-09-03T08:00:00Z',
  issueDate: '2026-09-03',
  net: -1513,
  vat: -287,
  gross: -1800,
  lines: [{ ...receiptCanceled.lines[0]!, unitPrice: -1800, net: -1513, vat: -287, gross: -1800 }],
  reason: 'Sold to the wrong person',
  canceledBy: undefined,
  cancels: { id: id(732), number: 'SD-2026-0001', issueDate: '2026-09-02' },
};

/** The receipts as GET /admin/receipts lists them, newest first. */
export const receipts: Receipt[] = [receiptCancellation, receiptSale, receiptCanceled];

/**
 * Complete receipt settings in the standard tax mode in euros (so receipts
 * over 250 € should name the address), with a PNG logo.
 */
export const receiptSettings = {
  complete: true,
  issuerName: issuer.name,
  issuerAddress: issuer.address,
  taxNumber: issuer.taxNumber,
  email: issuer.email,
  taxMode: 'standard' as const,
  receiptPrefix: 'SD',
  logoType: 'image/png' as const,
  logoMaxBytes: 524288,
  addressRequiredAbove: 25000,
};
