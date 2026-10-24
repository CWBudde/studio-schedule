import { hashKey, mutationOptions, type QueryClient, queryOptions } from '@tanstack/react-query';
import i18n from '@/i18n';
import {
  api,
  type ConfirmEmailBody,
  type LoginBody,
  type Me,
  type paths,
  type RegisterBody,
} from './client';
import type { components } from './schema';

/** Error thrown by query functions when the API answers with a non-2xx status. */
export class ApiError extends Error {
  readonly status: number;
  readonly body: unknown;
  /** Seconds to wait before asking again, from a Retry-After header (e.g. of a 429). */
  readonly retryAfter: number | undefined;

  constructor(status: number, body: unknown, retryAfter?: number) {
    super(`API request failed with status ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
    this.retryAfter = retryAfter;
  }

  static from(response: Response, body: unknown): ApiError {
    return new ApiError(
      response.status,
      body,
      parseRetryAfter(response.headers.get('Retry-After')),
    );
  }
}

/** The seconds of a Retry-After header: delay-seconds or an HTTP date. */
function parseRetryAfter(header: string | null): number | undefined {
  if (!header) return undefined;
  if (/^\d+$/.test(header.trim())) return Number(header.trim());
  const at = Date.parse(header);
  return Number.isNaN(at) ? undefined : Math.max(0, Math.ceil((at - Date.now()) / 1000));
}

/** The data of an openapi-fetch result; a non-2xx answer throws an ApiError. */
async function unwrap<T>(
  request: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await request;
  if (data === undefined) throw ApiError.from(response, error);
  return data;
}

/** Like unwrap, but an answer with status `absent` means "nothing there": null. */
async function unwrapOrNull<T>(
  absent: number,
  request: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T | null> {
  const { data, error, response } = await request;
  if (response.status === absent) return null;
  if (data === undefined) throw ApiError.from(response, error);
  return data;
}

/** Waits for an answer without a body (e.g. 202, 204); a non-2xx answer throws an ApiError. */
async function expectOk(request: Promise<{ error?: unknown; response: Response }>): Promise<void> {
  const { error, response } = await request;
  if (!response.ok) throw ApiError.from(response, error);
}

export const healthQuery = queryOptions({
  queryKey: ['health'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/health', { signal })),
});

export const publicSettingsQuery = queryOptions({
  queryKey: ['settings', 'public'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/settings/public', { signal })),
  staleTime: 5 * 60_000,
});

export type ScheduleParams = NonNullable<paths['/api/v1/schedule']['get']['parameters']['query']>;

/** Prefix of every schedule query key, e.g. to refetch all schedules at a release. */
export const scheduleQueryKey = ['schedule'] as const;

/** The published schedule of a range of school dates (`to` is exclusive, at most 42 days). */
export const scheduleQuery = (params: ScheduleParams) =>
  queryOptions({
    queryKey: [...scheduleQueryKey, params],
    queryFn: ({ signal }) =>
      unwrap(
        api.GET('/api/v1/schedule', {
          params: { query: params },
          signal,
        }),
      ),
  });

/** One published lesson. Unknown and invisible lessons fail alike with status 404. */
export const lessonQuery = (lessonId: string) =>
  queryOptions({
    queryKey: ['lesson', lessonId],
    queryFn: ({ signal }) =>
      unwrap(
        api.GET('/api/v1/lessons/{lessonId}', {
          params: { path: { lessonId } },
          signal,
        }),
      ),
  });

export const locationsQuery = queryOptions({
  queryKey: ['locations'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/locations', { signal }))).items,
  staleTime: 5 * 60_000,
});

export const staffQuery = queryOptions({
  queryKey: ['staff'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/staff', { signal }))).items,
  staleTime: 5 * 60_000,
});

export const courseTypesQuery = queryOptions({
  queryKey: ['course-types'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/course-types', { signal }))).items,
  staleTime: 5 * 60_000,
});

/** The rooms of all locations. */
export const roomsQuery = queryOptions({
  queryKey: ['rooms'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/rooms', { signal }))).items,
  staleTime: 5 * 60_000,
});

/** All terms, ordered by start date. */
export const termsQuery = queryOptions({
  queryKey: ['terms'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/terms', { signal }))).items,
  staleTime: 5 * 60_000,
});

/** The weekly classes of a term, ordered by weekday (Sunday first) and start time. */
export const classesQuery = (termId: string) =>
  queryOptions({
    queryKey: ['terms', termId, 'classes'],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.GET('/api/v1/terms/{termId}/classes', { params: { path: { termId } }, signal }),
        )
      ).items,
    staleTime: 5 * 60_000,
  });

/**
 * The signed-in account, or null when signed out. The API answers 401 then,
 * which is a normal state here and not an error.
 */
export const meQuery = queryOptions({
  queryKey: ['auth', 'me'],
  queryFn: ({ signal }) => unwrapOrNull(401, api.GET('/api/v1/auth/me', { signal })),
});

/** Prefix of the admin-only query keys (see listAccountsQuery). */
export const adminQueryKey = ['admin'] as const;

/**
 * Makes `me` the signed-in account (null: signed out) after a change of
 * identity, e.g. a sign-in or the start of an impersonation. What the API
 * answers depends on who asks (the schedule hides lessons from guests, for
 * example), so every other query is reset: it loses the data of the previous
 * identity and refetches if it is shown. Admin queries are dropped instead,
 * so a page that shows one does not ask again with the new identity before
 * it renders the change.
 */
function changeIdentity(queryClient: QueryClient, me: Me | null) {
  queryClient.removeQueries({ queryKey: adminQueryKey });
  queryClient.setQueryData(meQuery.queryKey, me);
  const meHash = hashKey(meQuery.queryKey);
  void queryClient.resetQueries({ predicate: (query) => query.queryHash !== meHash });
}

/**
 * A sign-in: `me` becomes the signed-in account, and the UI switches to the
 * account's language if it has one. Only a sign-in does that; refetching
 * `me` or signing in as someone else keeps the language the person chose.
 */
function signIn(queryClient: QueryClient, me: Me) {
  changeIdentity(queryClient, me);
  if (me.locale && me.locale !== i18n.resolvedLanguage) void i18n.changeLanguage(me.locale);
}

/** Signs in; the account becomes the `me` query's data and its language the UI's. */
export const loginMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: LoginBody) => unwrap(api.POST('/api/v1/auth/login', { body })),
    onSuccess: (me: Me) => signIn(queryClient, me),
  });

/** Confirms an email address with the link's token and the password, and signs in. */
export const confirmEmailMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: ConfirmEmailBody) => unwrap(api.POST('/api/v1/auth/confirm', { body })),
    onSuccess: (me: Me) => signIn(queryClient, me),
  });

/** Registers an account; the API mails a confirmation link (shown in the demo mailbox). */
export const registerMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: RegisterBody) => expectOk(api.POST('/api/v1/auth/register', { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey }),
  });

/** The JSON request body of an operation, as the generated schema has it. */
type JsonBody<P extends keyof paths, M extends 'post' | 'put'> = paths[P] extends {
  [m in M]: { requestBody: { content: { 'application/json': infer B } } };
}
  ? B
  : never;
type PostBody<P extends keyof paths> = JsonBody<P, 'post'>;
type PutBody<P extends keyof paths> = JsonBody<P, 'put'>;

export type RequestPasswordResetBody = PostBody<'/api/v1/auth/password-reset'>;
export type ResetPasswordBody = PostBody<'/api/v1/auth/password-reset/confirm'>;
export type ChangePasswordBody = PostBody<'/api/v1/auth/password'>;
export type RequestEmailChangeBody = PostBody<'/api/v1/auth/email'>;
export type ConfirmEmailChangeBody = PostBody<'/api/v1/auth/email/confirm'>;
export type UpdateLocaleBody = PutBody<'/api/v1/auth/locale'>;
/** Name, phone and birth date of the signed-in person or one of their dependents. */
export type PersonData = PutBody<'/api/v1/family/me'>;

export type Family =
  paths['/api/v1/family']['get']['responses'][200]['content']['application/json'];
export type FamilyPerson = Family['me'];
export type FamilyDependent = Family['dependents'][number];

/**
 * Asks the API again who is signed in after a change that may have ended
 * the session or changed the account (a password reset, a new address).
 * Nothing to do if nobody was signed in. A different answer is a change of
 * identity; a failure only marks `me` stale.
 */
async function refreshMe(queryClient: QueryClient): Promise<void> {
  const before = queryClient.getQueryData(meQuery.queryKey);
  if (!before) return;
  try {
    const me = await queryClient.fetchQuery({ ...meQuery, staleTime: 0 });
    if (me?.accountId !== before.accountId) changeIdentity(queryClient, me);
  } catch {
    void queryClient.invalidateQueries({ queryKey: meQuery.queryKey });
  }
}

/** A 401 of a signed-in operation means the session is over: nobody is signed in any more. */
function signedOutOn401(queryClient: QueryClient) {
  return (error: Error) => {
    if (error instanceof ApiError && error.status === 401) changeIdentity(queryClient, null);
  };
}

/**
 * Mails a link to set a new password (shown in the demo mailbox). The API
 * answers alike for every address.
 */
export const requestPasswordResetMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: RequestPasswordResetBody) =>
      expectOk(api.POST('/api/v1/auth/password-reset', { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey }),
  });

/**
 * Sets a new password with the token of a reset link. It does not sign in
 * and ends every session of the account, perhaps the one of this browser.
 */
export const resetPasswordMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: ResetPasswordBody) =>
      expectOk(api.POST('/api/v1/auth/password-reset/confirm', { body })),
    onSuccess: () => refreshMe(queryClient),
  });

/** Changes the signed-in account's password; its other sessions end, this one stays. */
export const changePasswordMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: ChangePasswordBody) => expectOk(api.POST('/api/v1/auth/password', { body })),
    onError: signedOutOn401(queryClient),
  });

/**
 * Mails a confirmation link to a new address of the signed-in account (shown
 * in the demo mailbox); the address changes once the link is opened.
 */
export const requestEmailChangeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: RequestEmailChangeBody) =>
      expectOk(api.POST('/api/v1/auth/email', { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey }),
    onError: signedOutOn401(queryClient),
  });

/**
 * Moves an account to its new address with the token of the mailed link
 * (signed in or not); the API mails a notice to the old address.
 */
export const confirmEmailChangeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: ConfirmEmailChangeBody) =>
      expectOk(api.POST('/api/v1/auth/email/confirm', { body })),
    onSuccess: async () => {
      void queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey });
      await refreshMe(queryClient);
    },
  });

/**
 * Sets the language of the mails the account receives; an empty body means
 * the school's default. The UI language stays as it is.
 */
export const updateLocaleMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: UpdateLocaleBody) => expectOk(api.PUT('/api/v1/auth/locale', { body })),
    onSuccess: (_: void, { locale }: UpdateLocaleBody) =>
      queryClient.setQueryData(meQuery.queryKey, (me) => {
        if (!me) return me;
        const { locale: _previous, ...rest } = me;
        return locale ? { ...rest, locale } : rest;
      }),
    onError: signedOutOn401(queryClient),
  });

/** The signed-in person and their dependents (children without their own login). */
export const familyQuery = queryOptions({
  queryKey: ['family'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/family', { signal })),
});

const refreshFamily = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: familyQuery.queryKey });

/**
 * The errors of a dependent operation. A 404 means the dependent left the
 * family meanwhile (another guardian or the office removed the link), so the
 * family is asked again.
 */
function onDependentError(queryClient: QueryClient) {
  const signedOut = signedOutOn401(queryClient);
  return (error: Error) => {
    signedOut(error);
    if (error instanceof ApiError && error.status === 404) void refreshFamily(queryClient);
  };
}

/** Changes the signed-in person's data; the header's name comes from `me`, so it is asked again. */
export const updateMyPersonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PersonData) => unwrap(api.PUT('/api/v1/family/me', { body })),
    onSuccess: () =>
      Promise.all([
        refreshFamily(queryClient),
        queryClient.invalidateQueries({ queryKey: meQuery.queryKey }),
      ]),
    onError: signedOutOn401(queryClient),
  });

/** Adds a child without their own login, with the signed-in person as guardian. */
export const addDependentMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PersonData) => unwrap(api.POST('/api/v1/family/dependents', { body })),
    onSuccess: () => refreshFamily(queryClient),
    onError: signedOutOn401(queryClient),
  });

/** Changes the data of one of the signed-in person's dependents (404 for anyone else). */
export const updateDependentMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, body }: { personId: string; body: PersonData }) =>
      unwrap(
        api.PUT('/api/v1/family/dependents/{personId}', { params: { path: { personId } }, body }),
      ),
    onSuccess: () => refreshFamily(queryClient),
    onError: onDependentError(queryClient),
  });

/**
 * Ends the signed-in person's guardianship of a dependent. One left without
 * a guardian is deleted, unless it has bookings or class memberships: then
 * the API answers 409 and nothing changes.
 */
export const removeDependentMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (personId: string) =>
      expectOk(
        api.DELETE('/api/v1/family/dependents/{personId}', { params: { path: { personId } } }),
      ),
    onSuccess: () => refreshFamily(queryClient),
    onError: onDependentError(queryClient),
  });

/** Signs out; the `me` query becomes null. */
export const logoutMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: () => expectOk(api.POST('/api/v1/auth/logout')),
    onSuccess: () => changeIdentity(queryClient, null),
  });

/** Every account with its person, ordered by email (admins only). */
export const listAccountsQuery = queryOptions({
  queryKey: [...adminQueryKey, 'accounts'],
  queryFn: async ({ signal }) =>
    (await unwrap(api.GET('/api/v1/admin/accounts', { signal }))).items,
});

/**
 * Signs the admin in as another account ("login as"); that account, with
 * the admin as its impersonator, becomes the `me` query's data.
 */
export const impersonateMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (accountId: string) =>
      unwrap(
        api.POST('/api/v1/admin/accounts/{accountId}/impersonate', {
          params: { path: { accountId } },
        }),
      ),
    onSuccess: (me: Me) => changeIdentity(queryClient, me),
  });

/**
 * Ends an impersonation; the admin's own account becomes the `me` query's
 * data again. A 401 means the impersonation is over already (it lasts an
 * hour): then nobody is signed in any more.
 */
export const stopImpersonationMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: () => unwrap(api.POST('/api/v1/auth/impersonation/stop')),
    onSuccess: (me: Me) => changeIdentity(queryClient, me),
    onError: (error) => {
      if (error instanceof ApiError && error.status === 401) changeIdentity(queryClient, null);
    },
  });

export type Location = components['schemas']['Location'];
export type Room = components['schemas']['Room'];
/** The editable fields of a location (create and update). */
export type LocationData = PostBody<'/api/v1/locations'>;
/** The editable fields of a room (create and update). */
export type RoomData = PostBody<'/api/v1/rooms'>;

export type CourseType = components['schemas']['CourseType'];
/** The editable fields of a course type (create and update). */
export type CourseTypeData = PostBody<'/api/v1/course-types'>;
/** A staff member as the office sees them: with the person link and the active flag. */
export type StaffEntry = components['schemas']['StaffEntry'];
/** The editable fields of a staff member (create and update); no `personId` means no person. */
export type StaffData = PostBody<'/api/v1/staff'>;
/** A person found by the office's person search. */
export type PersonMatch = components['schemas']['PersonMatch'];

/** Every staff member, inactive ones included, with their person link (office). */
export const allStaffQuery = queryOptions({
  queryKey: [...adminQueryKey, 'staff'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/admin/staff', { signal }))).items,
});

/**
 * The persons whose name or email contains every word of `q` (office), at
 * most 50, ordered by family and given name; an empty `q` matches everyone.
 */
export const personsQuery = (q: string) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'persons', q.trim()],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.GET('/api/v1/admin/persons', {
            params: { query: q.trim() ? { q: q.trim() } : {} },
            signal,
          }),
        )
      ).items,
    staleTime: 30_000,
  });

/**
 * After a write to the catalogue: the lists are asked again (the public ones
 * feed the schedule's filters), and so is what shows catalogue names (the
 * schedule, lessons, classes).
 */
function refreshCatalogue(queryClient: QueryClient) {
  return Promise.all(
    [
      locationsQuery.queryKey,
      roomsQuery.queryKey,
      courseTypesQuery.queryKey,
      staffQuery.queryKey,
      allStaffQuery.queryKey,
      scheduleQueryKey,
      ['lesson'],
      ['terms'],
    ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );
}

/**
 * The errors of a catalogue write. A 404 means someone else deleted the
 * record meanwhile, so the lists are asked again.
 */
function onCatalogueError(queryClient: QueryClient) {
  const signedOut = signedOutOn401(queryClient);
  return (error: Error) => {
    signedOut(error);
    if (error instanceof ApiError && error.status === 404) void refreshCatalogue(queryClient);
  };
}

/** Adds a location (office); a name another location has is a 409 `duplicate`. */
export const createLocationMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: LocationData) => unwrap(api.POST('/api/v1/locations', { body })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Replaces the name and address of a location (office). */
export const updateLocationMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ locationId, body }: { locationId: string; body: LocationData }) =>
      unwrap(api.PUT('/api/v1/locations/{locationId}', { params: { path: { locationId } }, body })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Deletes a location (office); one with rooms is a 409 `in-use`. */
export const deleteLocationMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (locationId: string) =>
      expectOk(api.DELETE('/api/v1/locations/{locationId}', { params: { path: { locationId } } })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Adds a room to a location (office); a name the location has is a 409 `duplicate`. */
export const createRoomMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: RoomData) => unwrap(api.POST('/api/v1/rooms', { body })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Replaces the location, name and capacity of a room (office). */
export const updateRoomMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ roomId, body }: { roomId: string; body: RoomData }) =>
      unwrap(api.PUT('/api/v1/rooms/{roomId}', { params: { path: { roomId } }, body })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Deletes a room (office); one used by classes or lessons is a 409 `in-use`. */
export const deleteRoomMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (roomId: string) =>
      expectOk(api.DELETE('/api/v1/rooms/{roomId}', { params: { path: { roomId } } })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Adds a course type (office); a name another one has (ignoring case) is a 409 `duplicate`. */
export const createCourseTypeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: CourseTypeData) => unwrap(api.POST('/api/v1/course-types', { body })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Replaces the name and description of a course type (office). */
export const updateCourseTypeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ courseTypeId, body }: { courseTypeId: string; body: CourseTypeData }) =>
      unwrap(
        api.PUT('/api/v1/course-types/{courseTypeId}', {
          params: { path: { courseTypeId } },
          body,
        }),
      ),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Deletes a course type (office); one used by classes or lessons is a 409 `in-use`. */
export const deleteCourseTypeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (courseTypeId: string) =>
      expectOk(
        api.DELETE('/api/v1/course-types/{courseTypeId}', {
          params: { path: { courseTypeId } },
        }),
      ),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Adds a staff member (office); an unknown person is a 422 on `body.personId`. */
export const createStaffMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: StaffData) => unwrap(api.POST('/api/v1/staff', { body })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/**
 * Replaces the name, kind, person link and active flag of a staff member
 * (office). Inactive staff leave the public staff list (and so the
 * schedule's filters) but stay on their lessons.
 */
export const updateStaffMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ staffId, body }: { staffId: string; body: StaffData }) =>
      unwrap(api.PUT('/api/v1/staff/{staffId}', { params: { path: { staffId } }, body })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/**
 * Deactivates a staff member (office), keeping their name, kind and person:
 * the way out when they cannot be deleted because they still teach.
 */
export const deactivateStaffMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: async ({ id, displayName, kind, person }: StaffEntry) => {
      await unwrap(
        api.PUT('/api/v1/staff/{staffId}', {
          params: { path: { staffId: id } },
          body: { displayName, kind, active: false, ...(person ? { personId: person.id } : {}) },
        }),
      );
    },
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** Deletes a staff member (office); one that classes or lessons refer to is a 409 `in-use`. */
export const deleteStaffMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (staffId: string) =>
      expectOk(api.DELETE('/api/v1/staff/{staffId}', { params: { path: { staffId } } })),
    onSuccess: () => refreshCatalogue(queryClient),
    onError: onCatalogueError(queryClient),
  });

/** A weekly class as the office sees it: with the retired flag. */
export type ClassEntry = components['schemas']['ClassEntry'];
/** The editable fields of a class (create and update); no `accompanistId` means none. */
export type ClassData = PostBody<'/api/v1/terms/{termId}/classes'>;

/**
 * Every weekly class of a term, retired ones included (office), in the
 * order of classesQuery. Under the term's key, so whatever refreshes the
 * terms refreshes it too.
 */
export const allClassesQuery = (termId: string) =>
  queryOptions({
    queryKey: [...classesQuery(termId).queryKey, 'all'],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.GET('/api/v1/admin/terms/{termId}/classes', {
            params: { path: { termId } },
            signal,
          }),
        )
      ).items,
  });

/** The request body that keeps a class as it is, but for `retired`. */
export const classDataOf = (c: ClassEntry, retired = c.retired): ClassData => ({
  courseTypeId: c.courseTypeId,
  roomId: c.roomId,
  instructorId: c.instructor.id,
  ...(c.accompanist ? { accompanistId: c.accompanist.id } : {}),
  level: c.level,
  weekday: c.weekday,
  startTime: c.startTime,
  durationMinutes: c.durationMinutes,
  capacity: c.capacity,
  retired,
});

/**
 * After a write to a class: its lessons changed as well, so the classes
 * (under the terms' key), the schedule and the lessons are asked again.
 */
function refreshTimetable(queryClient: QueryClient) {
  return Promise.all(
    [['terms'], scheduleQueryKey, ['lesson']].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

/** The errors of a class write; a 404 means the class is gone meanwhile. */
function onClassError(queryClient: QueryClient) {
  const signedOut = signedOutOn401(queryClient);
  return (error: Error) => {
    signedOut(error);
    if (error instanceof ApiError && error.status === 404) void refreshTimetable(queryClient);
  };
}

/**
 * Adds a weekly class to a term (office); the API generates its lessons
 * ahead. An unknown course type, room or staff member is a 422 on its field.
 */
export const createClassMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ termId, body }: { termId: string; body: ClassData }) =>
      unwrap(api.POST('/api/v1/terms/{termId}/classes', { params: { path: { termId } }, body })),
    onSuccess: () => refreshTimetable(queryClient),
    onError: onClassError(queryClient),
  });

/**
 * Replaces the fields of a class (office); its term stays. The coming
 * lessons that were not edited one by one follow, with their bookings.
 */
export const updateClassMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ classId, body }: { classId: string; body: ClassData }) =>
      unwrap(api.PUT('/api/v1/classes/{classId}', { params: { path: { classId } }, body })),
    onSuccess: () => refreshTimetable(queryClient),
    onError: onClassError(queryClient),
  });

/** Sets the retired flag of a class (office), keeping everything else. */
const classRetiredMutation = (retired: boolean) => (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: async (c: ClassEntry) => {
      await unwrap(
        api.PUT('/api/v1/classes/{classId}', {
          params: { path: { classId: c.id } },
          body: classDataOf(c, retired),
        }),
      );
    },
    onSuccess: () => refreshTimetable(queryClient),
    onError: onClassError(queryClient),
  });

/**
 * Retires a class (office): it generates no more lessons, its coming
 * lessons that still follow it and have no bookings go, and it leaves the
 * public timetable. The way out when a class with history cannot be
 * deleted.
 */
export const retireClassMutation = classRetiredMutation(true);

/** Makes a retired class generate lessons again and shows it in the public timetable. */
export const reactivateClassMutation = classRetiredMutation(false);

/**
 * Deletes a class with its lessons (office); one with members, booked or
 * started lessons is a 409 `in-use`.
 */
export const deleteClassMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (classId: string) =>
      expectOk(api.DELETE('/api/v1/classes/{classId}', { params: { path: { classId } } })),
    onSuccess: () => refreshTimetable(queryClient),
    onError: onClassError(queryClient),
  });

/**
 * The mails of the demo mailbox, newest first; null when the installation is
 * not a demo (404). In a demo it is refetched whenever a view of it mounts;
 * elsewhere the 404 is final and not asked again.
 */
export const demoMailboxQuery = queryOptions({
  queryKey: ['demo', 'mailbox'],
  queryFn: async ({ signal }) =>
    (await unwrapOrNull(404, api.GET('/api/v1/demo/mailbox', { signal })))?.mails ?? null,
  staleTime: (query) => (query.state.data === null ? Infinity : 0),
});

/** The demo accounts, one per role; null when the installation is not a demo (404). */
export const demoLoginsQuery = queryOptions({
  queryKey: ['demo', 'logins'],
  queryFn: async ({ signal }) =>
    (await unwrapOrNull(404, api.GET('/api/v1/demo/logins', { signal })))?.logins ?? null,
  staleTime: Infinity,
});
