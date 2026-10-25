import {
  hashKey,
  infiniteQueryOptions,
  MutationCache,
  mutationOptions,
  QueryCache,
  type QueryClient,
  queryOptions,
} from '@tanstack/react-query';
import { chooseLanguage, supportedLanguage } from '@/i18n';
import {
  type DeviceSubscription,
  deviceLabel,
  pushDevice,
  pushOwner,
  setPushOwner,
} from '@/features/push/device';
import { clearOfflineCache } from '@/lib/offlineCache';
import {
  api,
  type ConfirmEmailBody,
  type LoginBody,
  type Me,
  type paths,
  type RegisterBody,
  type Schedule,
  type ScheduleLesson,
} from './client';
import { conflictReason } from './conflict';
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

/** The school's logo for the header, or null without one (public, revalidated with its ETag). */
export const schoolLogoQuery = queryOptions({
  queryKey: ['settings', 'logo'],
  queryFn: ({ signal }) =>
    unwrapOrNull(
      404,
      api.GET('/api/v1/settings/logo', { parseAs: 'blob', signal }),
    ) as Promise<Blob | null>,
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

/** A lesson of the instructor view (/teach), with the caller's role and the counts. */
export type TeachLesson = components['schemas']['TeachLesson'];
/** The lessons of the instructor view in a range of school dates. */
export type TeachLessons = components['schemas']['TeachLessons'];
export type TeachParams = NonNullable<paths['/api/v1/teach']['get']['parameters']['query']>;

/** Prefix of every instructor-view query key (refreshed after every lesson or booking write). */
export const teachQueryKey = ['teach'] as const;

/**
 * The lessons a staff member teaches that start in a range of school dates
 * (`to` is exclusive, at most 31 days; without `from` the week before today
 * through the week after, as the server's clock has it). The office gets
 * every lesson, or with `staffId` one staff member's.
 */
export const teachLessonsQuery = (params: TeachParams) =>
  queryOptions({
    queryKey: [...teachQueryKey, params],
    queryFn: ({ signal }) =>
      unwrap(api.GET('/api/v1/teach', { params: { query: params }, signal })),
  });

/** A booking of the signed-in family in "My lessons", with its lesson and whether it may be canceled. */
export type MyLessonBooking = components['schemas']['MyLessonBooking'];
/** An open waiting-list entry of the signed-in family in "My lessons", with its lesson. */
export type MyWaitlistEntry = components['schemas']['MyWaitlistEntry'];
/** The family's coming lessons and open waiting-list entries (listMyLessons). */
export type MyLessons = components['schemas']['MyLessons'];
/** A page of the family's past bookings (three calendar months). */
export type MyLessonHistory = components['schemas']['MyLessonHistory'];

/** Prefix of every "My lessons" query key (refreshed after every lesson or booking write). */
export const myLessonsQueryKey = ['my-lessons'] as const;

/**
 * The family's coming lessons (`upcoming`: active bookings of lessons that
 * are not over) and its open waiting-list entries, with the server's `now`.
 */
export const myLessonsQuery = queryOptions({
  queryKey: [...myLessonsQueryKey, 'upcoming'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/my/lessons', { signal })),
});

/**
 * The family's past bookings, newest first, a page of three calendar months
 * at a time: the first page ends now, each next one at the previous page's
 * `nextBefore`, as long as `hasMore` says older bookings exist. A page may
 * be empty while there are older ones.
 */
export const myLessonHistoryQuery = infiniteQueryOptions({
  queryKey: [...myLessonsQueryKey, 'history'],
  queryFn: ({ signal, pageParam }) =>
    unwrap(
      api.GET('/api/v1/my/lessons/history', {
        params: { query: pageParam ? { before: pageParam } : {} },
        signal,
      }),
    ),
  initialPageParam: '',
  getNextPageParam: (last: MyLessonHistory) => (last.hasMore ? last.nextBefore : undefined),
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
 * it renders the change. The offline copy of the cache goes too (ADR 0017):
 * it held the previous identity's lessons.
 */
function changeIdentity(queryClient: QueryClient, me: Me | null) {
  void clearOfflineCache();
  queryClient.removeQueries({ queryKey: adminQueryKey });
  queryClient.setQueryData(meQuery.queryKey, me);
  const meHash = hashKey(meQuery.queryKey);
  void queryClient.resetQueries({ predicate: (query) => query.queryHash !== meHash });
}

/**
 * A sign-in: `me` becomes the signed-in account, and the UI switches to the
 * account's language if it has one, as the visitor's choice: it outranks
 * the school's default language from then on (ADR 0013). Only a sign-in
 * does that; refetching `me` or signing in as someone else keeps the
 * language the person chose.
 */
function signIn(queryClient: QueryClient, me: Me) {
  changeIdentity(queryClient, me);
  const lng = supportedLanguage(me.locale);
  if (lng) void chooseLanguage(lng);
}

/** Signs in; the account becomes the `me` query's data and its language the UI's. */
export const loginMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: LoginBody) => unwrap(api.POST('/api/v1/auth/login', { body })),
    onSuccess: (me: Me) => signIn(queryClient, me),
    // A 401 is a wrong email or password.
    meta: { credentialCheck: true },
  });

/** Confirms an email address with the link's token and the password, and signs in. */
export const confirmEmailMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: ConfirmEmailBody) => unwrap(api.POST('/api/v1/auth/confirm', { body })),
    onSuccess: (me: Me) => signIn(queryClient, me),
    // A 401 is a wrong password.
    meta: { credentialCheck: true },
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

declare module '@tanstack/react-query' {
  interface Register {
    mutationMeta: {
      /**
       * The operation checks a password, and its 401 means a wrong one
       * (login, confirmEmail), not that the session is over: it does not
       * sign out (sessionCaches).
       */
      credentialCheck?: boolean;
    };
  }
}

/**
 * A 401 means the session is over (it expired, or ended on another device):
 * nobody is signed in any more, and the data of the account leaves the
 * cache (ASVS 8.2.3). Once `me` is null, a further 401 changes nothing, so
 * the queries that refetch after the reset cannot start it again. `getMe`
 * answers a guest's 401 as null, not as an error.
 */
function signOutOn401(queryClient: QueryClient, error: Error) {
  if (!(error instanceof ApiError) || error.status !== 401) return;
  if (queryClient.getQueryData(meQuery.queryKey) === null) return;
  changeIdentity(queryClient, null);
}

/**
 * The query and mutation caches of the app's QueryClient: a 401 of any
 * query or mutation signs out (signOutOn401), except a mutation's whose meta
 * marks a `credentialCheck`. This is the one place a 401 is handled. `client`
 * returns the QueryClient they belong to.
 */
export function sessionCaches(client: () => QueryClient) {
  return {
    queryCache: new QueryCache({ onError: (error) => signOutOn401(client(), error) }),
    mutationCache: new MutationCache({
      onError: (error, _variables, _context, mutation) => {
        if (!mutation.meta?.credentialCheck) signOutOn401(client(), error);
      },
    }),
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
export const changePasswordMutation = mutationOptions({
  mutationFn: (body: ChangePasswordBody) => expectOk(api.POST('/api/v1/auth/password', { body })),
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
 * family meanwhile (another guardian or the office removed the link), and a
 * 409 `managed` that it got an account, a staff link or dependents of its own
 * meanwhile and is read-only now: either way, the family is asked again.
 */
function onDependentError(queryClient: QueryClient) {
  return (error: Error) => {
    const gone = error instanceof ApiError && error.status === 404;
    if (gone || conflictReason(error) === 'managed') void refreshFamily(queryClient);
  };
}

/**
 * Changes the signed-in person's data; the header's name comes from `me`,
 * so it is asked again, and so is everything that shows the family's names
 * (the lessons' bookings and waiting lists, "My lessons").
 */
export const updateMyPersonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PersonData) => unwrap(api.PUT('/api/v1/family/me', { body })),
    onSuccess: () => {
      void refreshLessons(queryClient);
      return Promise.all([
        refreshFamily(queryClient),
        queryClient.invalidateQueries({ queryKey: meQuery.queryKey }),
      ]);
    },
  });

/** Adds a child without their own login, with the signed-in person as guardian. */
export const addDependentMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PersonData) => unwrap(api.POST('/api/v1/family/dependents', { body })),
    onSuccess: () => refreshFamily(queryClient),
  });

/**
 * Changes the data of one of the signed-in person's dependents (404 for
 * anyone else); a managed dependent (own account, staff link, or a guardian
 * of someone itself) is a 409 `managed`. What shows the family's names (the
 * lessons, "My lessons") is asked again.
 */
export const updateDependentMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, body }: { personId: string; body: PersonData }) =>
      unwrap(
        api.PUT('/api/v1/family/dependents/{personId}', { params: { path: { personId } }, body }),
      ),
    onSuccess: () => {
      // Not awaited: the caller's own onSuccess runs once the family is
      // fresh, before the dependent's row (and its callback) may unmount.
      void refreshLessons(queryClient);
      return refreshFamily(queryClient);
    },
    onError: onDependentError(queryClient),
  });

/**
 * Ends the signed-in person's guardianship of a dependent. One left without
 * a guardian is deleted, unless it has bookings or class memberships: then
 * the API answers 409 `in-use` and nothing changes. A managed dependent (own
 * account, staff link, or a guardian of someone itself) is never deleted,
 * only unlinked.
 */
export const removeDependentMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (personId: string) =>
      expectOk(
        api.DELETE('/api/v1/family/dependents/{personId}', { params: { path: { personId } } }),
      ),
    // Removing a dependent ends their waiting-list entries, and their
    // bookings leave the lessons' family view and "My lessons".
    onSuccess: () => {
      // Not awaited: the caller's own onSuccess runs once the family is
      // fresh, before the dependent's row (and its callback) may unmount.
      void refreshLessons(queryClient);
      return refreshFamily(queryClient);
    },
    onError: onDependentError(queryClient),
  });

/** Signs out; the `me` query becomes null. */
export const logoutMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: async () => {
      // This device stops getting the account's notifications (ADR 0017).
      await forgetPushDevice();
      await expectOk(api.POST('/api/v1/auth/logout'));
    },
    onSuccess: () => changeIdentity(queryClient, null),
  });

/** Every account with its person, ordered by email (admins only). */
export const listAccountsQuery = queryOptions({
  queryKey: [...adminQueryKey, 'accounts'],
  queryFn: async ({ signal }) =>
    (await unwrap(api.GET('/api/v1/admin/accounts', { signal }))).items,
});

/**
 * The latest 200 audit events, newest first, with the accounts they name
 * (admins only). Always stale, so that the page shows new events whenever
 * it is opened or the window regains focus.
 */
export const listAuditEventsQuery = queryOptions({
  queryKey: [...adminQueryKey, 'audit'],
  staleTime: 0,
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/admin/audit', { signal }))).items,
});

// ------------------------------------------------- announcements (ADR 0014)

/** An announcement shown to the signed-in account. */
export type MyAnnouncement = components['schemas']['AnnouncementItem'];
/** An announcement as the office lists it. */
export type Announcement = components['schemas']['Announcement'];
/** What the office writes: the audience, title, text, window and email. */
export type AnnouncementData = PostBody<'/api/v1/admin/announcements'>;
/** Whom an announcement is for. */
export type AnnouncementAudience = PostBody<'/api/v1/admin/announcements/recipients'>;

/** Prefix of the signed-in account's announcement queries. */
export const myAnnouncementsQueryKey = ['announcements'] as const;

/**
 * The announcements shown now that reach the signed-in account, newest
 * first, dismissed ones included. Asked again every few minutes, so that a
 * new one shows without a reload.
 */
export const myAnnouncementsQuery = queryOptions({
  queryKey: [...myAnnouncementsQueryKey, 'mine'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/announcements', { signal }))).items,
  staleTime: 60_000,
  refetchInterval: 5 * 60_000,
});

/**
 * Hides an announcement from the signed-in account's banner (it stays in
 * its list). While an admin is signed in as the account, the API stores
 * nothing; the banner hides it for the session.
 */
export const dismissAnnouncementMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (announcementId: string) =>
      expectOk(
        api.POST('/api/v1/announcements/{announcementId}/dismiss', {
          params: { path: { announcementId } },
        }),
      ),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: myAnnouncementsQueryKey }),
  });

/** Every announcement, newest publication first (office). */
export const allAnnouncementsQuery = queryOptions({
  queryKey: [...adminQueryKey, 'announcements'],
  queryFn: async ({ signal }) =>
    (await unwrap(api.GET('/api/v1/admin/announcements', { signal }))).items,
});

/**
 * How many addresses an announcement for the audience would be mailed to
 * now (office), for the confirmation before emailing. Always asked anew.
 */
export const announcementRecipientsQuery = (audience: AnnouncementAudience) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'announcements', 'recipients', audience],
    queryFn: async () =>
      (await unwrap(api.POST('/api/v1/admin/announcements/recipients', { body: audience })))
        .recipients,
    staleTime: 0,
    gcTime: 0,
  });

/** After a write: both lists, and the mails it may have queued. */
function refreshAnnouncements(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: allAnnouncementsQuery.queryKey }),
    queryClient.invalidateQueries({ queryKey: myAnnouncementsQueryKey }),
    queryClient.invalidateQueries({ queryKey: [...adminQueryKey, 'mails'] }),
    queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey }),
  ]);
}

/**
 * Publishes an announcement; with `email`, it is mailed at once or at its
 * publication. A school day full of emailed announcements is a 409
 * `mail-limit`; an unknown class, course type or lesson a 422 on its field.
 */
export const createAnnouncementMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: AnnouncementData) =>
      unwrap(api.POST('/api/v1/admin/announcements', { body })),
    onSuccess: () => refreshAnnouncements(queryClient),
  });

/** Changes an announcement until it expires (409 `expired`); it is never mailed again. */
export const updateAnnouncementMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ announcementId, body }: { announcementId: string; body: AnnouncementData }) =>
      unwrap(
        api.PUT('/api/v1/admin/announcements/{announcementId}', {
          params: { path: { announcementId } },
          body,
        }),
      ),
    onSuccess: () => refreshAnnouncements(queryClient),
  });

/** Deletes an announcement with its dismissals. */
export const deleteAnnouncementMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (announcementId: string) =>
      expectOk(
        api.DELETE('/api/v1/admin/announcements/{announcementId}', {
          params: { path: { announcementId } },
        }),
      ),
    onSuccess: () => refreshAnnouncements(queryClient),
  });

// ---------------------------------------------------- mail log (ADR 0014)

/** A mail of the log, without its bodies. */
export type MailLogItem = components['schemas']['MailLogItem'];
/** A mail template, as the API names it (the enum of mail.TemplateNames). */
export type MailTemplate = NonNullable<MailLogItem['template']>;
/** A mail of the log with its bodies, where they are shown. */
export type MailLogDetail = components['schemas']['MailLogDetail'];
/** The filters of the mail log. */
export type MailLogFilter = Omit<PostBody<'/api/v1/admin/mails/search'>, 'cursor'>;

/**
 * The mail log, newest first, page by page (admins only). Always stale, so
 * that it shows new mails whenever it is opened. A POST, so that the
 * recipient searched for stays out of URLs (ASVS 8.3.1).
 */
export const mailLogQuery = (filter: MailLogFilter) =>
  infiniteQueryOptions({
    queryKey: [...adminQueryKey, 'mails', 'list', filter],
    initialPageParam: '',
    staleTime: 0,
    queryFn: ({ pageParam, signal }) =>
      unwrap(
        api.POST('/api/v1/admin/mails/search', {
          body: { ...filter, ...(pageParam ? { cursor: pageParam } : {}) },
          signal,
        }),
      ),
    getNextPageParam: (last) => last.nextCursor,
  });

/** One mail of the log, with its bodies unless withheld or purged (admins only). */
export const mailQuery = (mailId: string) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'mails', 'detail', mailId],
    queryFn: ({ signal }) =>
      unwrap(api.GET('/api/v1/admin/mails/{mailId}', { params: { path: { mailId } }, signal })),
  });

/**
 * Queues a failed mail again (admins only): a pending or sent one is a 409
 * `not-failed`, one with a secret link a 409 `secret-link`.
 */
export const requeueMailMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (mailId: string) =>
      unwrap(api.POST('/api/v1/admin/mails/{mailId}/requeue', { params: { path: { mailId } } })),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: [...adminQueryKey, 'mails'] }),
        queryClient.invalidateQueries({ queryKey: listAuditEventsQuery.queryKey }),
      ]),
  });

// ------------------------------------- import, duplicates, merge (ADR 0015)

/** A file for the import: its name and its bytes, base64 encoded. */
export type ImportFileBody = components['schemas']['ImportFileBody'];
/** Which column fills which field, and the columns that go into the notes. */
export type ImportMapping = components['schemas']['ImportMapping'];
/** The target fields of the import. */
export type ImportField = keyof ImportMapping['columns'];
/** What the import makes of a file before the mapping. */
export type ImportInspection = components['schemas']['ImportInspection'];
/** What an import does, or would do, with every row. */
export type ImportResult = components['schemas']['ImportResult'];
export type ImportRow = components['schemas']['ImportRow'];
export type ImportRunEntry = components['schemas']['ImportRunEntry'];

/** Reads a file's headers and first rows, and the mapping the headers suggest (office). */
export const inspectImportMutation = () =>
  mutationOptions({
    mutationFn: (body: ImportFileBody) =>
      unwrap(api.POST('/api/v1/admin/imports/inspect', { body })),
  });

/**
 * Imports the members of a file (office): a dry run that writes nothing, or
 * with `apply` the import, which the run history records.
 */
export const runImportMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PostBody<'/api/v1/admin/imports'>) =>
      unwrap(api.POST('/api/v1/admin/imports', { body })),
    onSuccess: (result: ImportResult) => (result.applied ? refreshPersons(queryClient) : undefined),
  });

/** The latest applied imports, newest first (office). */
export const importRunsQuery = queryOptions({
  queryKey: [...adminQueryKey, 'imports'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/admin/imports', { signal }))).items,
});

/** A pair of persons that may be the same. */
export type DuplicatePair = components['schemas']['DuplicatePair'];
/** A person of a pair. */
export type DuplicateCandidate = components['schemas']['DuplicateCandidate'];
/** The fields the survivor of a merge takes from the duplicate. */
export type MergeTake = components['schemas']['MergeTake'];
/** What a merge does, or would do. */
export type MergeSummary = components['schemas']['MergeSummary'];

/** The pairs of persons that may be duplicates, strong ones first (office). Always stale. */
export const duplicatesQuery = queryOptions({
  queryKey: [...adminQueryKey, 'duplicates'],
  staleTime: 0,
  queryFn: async ({ signal }) =>
    (await unwrap(api.GET('/api/v1/admin/duplicates', { signal }))).items,
});

/** Records that two persons are not the same, so the pair is no longer listed (office). */
export const dismissDuplicateMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: { personA: string; personB: string }) =>
      expectOk(api.POST('/api/v1/admin/duplicates/dismissals', { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: duplicatesQuery.queryKey }),
  });

/** The persons and fields of a merge. */
export type MergeRequest = Omit<PostBody<'/api/v1/admin/persons/merge'>, 'apply'>;

/**
 * What a merge would do, asked anew for every choice of the survivor and
 * the fields (office); it changes nothing. Never cached.
 */
export const mergePreviewQuery = (body: MergeRequest) =>
  queryOptions({
    // Apart from the admin key: a merge refreshes that, and the preview of
    // the merged pair is not to be asked again.
    queryKey: ['merge-preview', body],
    gcTime: 0,
    staleTime: 0,
    retry: false,
    queryFn: ({ signal }) =>
      unwrap(api.POST('/api/v1/admin/persons/merge', { body: { ...body, apply: false }, signal })),
  });

/**
 * Merges a duplicate into the survivor (office); without `apply` it only
 * tells what would happen. Both with an account is a 409 `two-accounts`, a
 * guardian cycle a 409 `guardian-cycle`.
 */
export const mergePersonsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PostBody<'/api/v1/admin/persons/merge'>) =>
      unwrap(api.POST('/api/v1/admin/persons/merge', { body })),
    onSuccess: (summary: MergeSummary) =>
      summary.applied ? refreshPersons(queryClient) : undefined,
    onError: onPersonError(queryClient),
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
 * A POST, so that the search terms stay out of URLs (ASVS 8.3.1).
 */
export const personsQuery = (q: string) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'persons', q.trim()],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.POST('/api/v1/admin/persons/search', {
            body: q.trim() ? { query: q.trim() } : {},
            signal,
          }),
        )
      ).items,
    staleTime: 30_000,
  });

/** Every field of a person, as the office sees it. */
export type AdminPerson = components['schemas']['AdminPerson'];
/** The fields of a person the office writes (create and update). */
export type PersonRecord = components['schemas']['PersonRecord'];
/** A person with its family links, account, staff links and class memberships (office). */
export type PersonDetail = components['schemas']['PersonDetail'];
/** The account of a person. */
export type PersonAccount = components['schemas']['PersonAccount'];
/** A new account of an existing person: its email and role. */
export type PersonAccountData = PostBody<'/api/v1/admin/persons/{personId}/account'>;
/** A class membership of a person, as the person's detail lists it. */
export type PersonClass = components['schemas']['PersonClass'];

/** A person with their guardians, dependents, account, staff links and classes (office). */
export const personQuery = (personId: string) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'person', personId],
    queryFn: ({ signal }) =>
      unwrap(
        api.GET('/api/v1/admin/persons/{personId}', { params: { path: { personId } }, signal }),
      ),
  });

/**
 * After a write to persons: what the office sees of them is asked again
 * (searches, details and the lists that name persons, all under the admin
 * key), and so are the family and the signed-in account, which may name the
 * person written.
 */
function refreshPersons(queryClient: QueryClient, { except }: { except?: string } = {}) {
  // A deleted person's page is left; asking for it again would only find it gone.
  const gone = except === undefined ? undefined : hashKey(personQuery(except).queryKey);
  return Promise.all(
    [adminQueryKey, familyQuery.queryKey, meQuery.queryKey].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey, predicate: (query) => query.queryHash !== gone }),
    ),
  );
}

/**
 * The errors of a write to a person. A 404 means the person (or the link)
 * is gone meanwhile, and a 409 that something changed meanwhile, such as a
 * guardian or an account added elsewhere: either way, the persons are asked
 * again.
 */
function onPersonError(queryClient: QueryClient) {
  return (error: Error) => {
    if (error instanceof ApiError && (error.status === 404 || error.status === 409)) {
      void refreshPersons(queryClient);
    }
  };
}

/** Adds a person without an account (office). */
export const createPersonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PersonRecord) => unwrap(api.POST('/api/v1/admin/persons', { body })),
    onSuccess: () => refreshPersons(queryClient),
    onError: onPersonError(queryClient),
  });

/**
 * Replaces every field of a person (office), including the email and the
 * notes; the email of the person's account, its login name, stays.
 */
export const updatePersonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, body }: { personId: string; body: PersonRecord }) =>
      unwrap(api.PUT('/api/v1/admin/persons/{personId}', { params: { path: { personId } }, body })),
    onSuccess: () => refreshPersons(queryClient),
    onError: onPersonError(queryClient),
  });

/**
 * Deletes a person (office), e.g. one added by mistake. While an account, a
 * staff link, bookings, class memberships, passes they own or paid for, or
 * a guardian link in either direction refer to the person, it is a 409
 * `in-use` and nothing changes.
 */
export const deletePersonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (personId: string) =>
      expectOk(api.DELETE('/api/v1/admin/persons/{personId}', { params: { path: { personId } } })),
    onSuccess: (_: void, personId: string) => refreshPersons(queryClient, { except: personId }),
    onError: onPersonError(queryClient),
  });

/**
 * Links a guardian to a person (office); a link that exists is a 409
 * `duplicate`. An unknown guardian, the person itself or one of its
 * dependents (at any depth, which would make a cycle) is a 422 on
 * `body.guardianId`.
 */
export const addGuardianMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, guardianId }: { personId: string; guardianId: string }) =>
      unwrap(
        api.POST('/api/v1/admin/persons/{personId}/guardians', {
          params: { path: { personId } },
          body: { guardianId },
        }),
      ),
    onSuccess: () => refreshPersons(queryClient),
    onError: onPersonError(queryClient),
  });

/** Ends a guardianship (office); the person stays, even without a guardian left. */
export const removeGuardianMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, guardianId }: { personId: string; guardianId: string }) =>
      expectOk(
        api.DELETE('/api/v1/admin/persons/{personId}/guardians/{guardianId}', {
          params: { path: { personId, guardianId } },
        }),
      ),
    onSuccess: () => refreshPersons(queryClient),
    onError: onPersonError(queryClient),
  });

/**
 * Creates an account for a person (office) and mails it an invitation (shown
 * in the demo mailbox). A person with an account, or an email another
 * account has, is a 409 `duplicate`; an admin account from the office a 403.
 */
export const createPersonAccountMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, body }: { personId: string; body: PersonAccountData }) =>
      unwrap(
        api.POST('/api/v1/admin/persons/{personId}/account', {
          params: { path: { personId } },
          body,
        }),
      ),
    onSuccess: () =>
      Promise.all([
        refreshPersons(queryClient),
        queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey }),
      ]),
    onError: onPersonError(queryClient),
  });

// ------------------------------- privacy and account administration (ADR 0016)

/** Everything the school stores about one person, as the export downloads it. */
export type PersonExport = components['schemas']['PersonExport'];
/** What anonymizing a person did. */
export type AnonymizeResult = components['schemas']['AnonymizeResult'];
/** A role an account can have. */
export type AccountRole = components['schemas']['AccountEntry']['role'];

/** Fetches the data export of any person (office); a merged or anonymized one too. */
export const exportPersonMutation = mutationOptions({
  mutationFn: (personId: string) =>
    unwrap(api.GET('/api/v1/admin/persons/{personId}/export', { params: { path: { personId } } })),
});

/**
 * Fetches the data export of the signed-in person or one of their
 * dependents; anyone else is a 404.
 */
export const exportMyDataMutation = mutationOptions({
  mutationFn: (personId: string) =>
    unwrap(api.GET('/api/v1/my/export', { params: { query: { person: personId } } })),
});

/**
 * Anonymizes a person (office): the personal data goes, the history stays.
 * The only guardian of a dependent is a 409 `sole-guardian`, an anonymized
 * person a 409 `anonymized`, one whose account must be kept a 409 `in-use`.
 * Its future bookings are canceled, so the lessons are asked again too.
 */
export const anonymizePersonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (personId: string) =>
      unwrap(
        api.POST('/api/v1/admin/persons/{personId}/anonymize', { params: { path: { personId } } }),
      ),
    onSuccess: () => {
      void refreshLessons(queryClient);
      return refreshPersons(queryClient);
    },
    onError: onPersonError(queryClient),
  });

/** After a change of an account: the accounts, the audit log and the persons are asked again. */
const refreshAdmin = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: adminQueryKey });

/** The errors of the account administration: a 404 or 409 asks for the accounts again. */
function onAccountError(queryClient: QueryClient) {
  return (error: Error) => {
    if (error instanceof ApiError && (error.status === 404 || error.status === 409)) {
      void refreshAdmin(queryClient);
    }
  };
}

/**
 * Disables or enables an account (admin). Disabling ends its sessions. The
 * admin's own account is a 409 `own-account`, the last admin who can sign in a 409
 * `last-admin`.
 */
export const setAccountDisabledMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ accountId, disabled }: { accountId: string; disabled: boolean }) =>
      unwrap(
        disabled
          ? api.POST('/api/v1/admin/accounts/{accountId}/disable', {
              params: { path: { accountId } },
            })
          : api.POST('/api/v1/admin/accounts/{accountId}/enable', {
              params: { path: { accountId } },
            }),
      ),
    onSuccess: () => refreshAdmin(queryClient),
    onError: onAccountError(queryClient),
  });

/** Gives an account another role (admin); the guards are those of disabling. */
export const setAccountRoleMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ accountId, role }: { accountId: string; role: AccountRole }) =>
      unwrap(
        api.PUT('/api/v1/admin/accounts/{accountId}/role', {
          params: { path: { accountId } },
          body: { role },
        }),
      ),
    onSuccess: () => refreshAdmin(queryClient),
    onError: onAccountError(queryClient),
  });

/**
 * Deletes an account (admin); its person stays. One that receipts, the audit
 * log, announcements or duplicate dismissals name is a 409 `in-use`.
 */
export const deleteAccountMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (accountId: string) =>
      expectOk(
        api.DELETE('/api/v1/admin/accounts/{accountId}', { params: { path: { accountId } } }),
      ),
    onSuccess: () => refreshAdmin(queryClient),
    onError: onAccountError(queryClient),
  });

/** The request of deleting the signed-in account. */
export type DeleteMyAccountBody = NonNullable<
  paths['/api/v1/my/account']['delete']['requestBody']
>['content']['application/json'];

/**
 * Deletes the signed-in account after checking its password (a wrong one is
 * a 422 on `body.password`) and signs out; a confirmation goes to its
 * address (shown in the demo mailbox).
 */
export const deleteMyAccountMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: DeleteMyAccountBody) => expectOk(api.DELETE('/api/v1/my/account', { body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey });
      changeIdentity(queryClient, null);
    },
  });

/**
 * After a write to the catalogue: the lists are asked again (the public ones
 * feed the schedule's filters), and so is what shows catalogue names (the
 * schedule, the instructor view, "My lessons", lessons, classes); a staff member's person
 * link also decides whose lessons the instructor view lists.
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
      teachQueryKey,
      myLessonsQueryKey,
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
  return (error: Error) => {
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
 * (under the terms' key), the schedule, the instructor view, "My lessons"
 * and the lessons are asked again.
 */
function refreshTimetable(queryClient: QueryClient) {
  return Promise.all(
    [['terms'], scheduleQueryKey, teachQueryKey, myLessonsQueryKey, ['lesson']].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

/**
 * The errors of a write to a term or class; a 404 means the record is gone
 * meanwhile, so the timetable is asked again.
 */
function onTimetableError(queryClient: QueryClient) {
  return (error: Error) => {
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
    onError: onTimetableError(queryClient),
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
    onError: onTimetableError(queryClient),
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
    onError: onTimetableError(queryClient),
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
    onError: onTimetableError(queryClient),
  });

/** The editable fields of a term (create and update). */
export type TermData = PostBody<'/api/v1/terms'>;

/** Adds a term (office); terms may overlap, and their names need not be unique. */
export const createTermMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: TermData) => unwrap(api.POST('/api/v1/terms', { body })),
    onSuccess: () => refreshTimetable(queryClient),
    onError: onTimetableError(queryClient),
  });

/**
 * Replaces the name and dates of a term (office). The coming lessons that
 * the new dates leave out go; a lesson left out that has started, has
 * bookings or was edited is a 422 on `startsOn` or `endsOn`.
 */
export const updateTermMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ termId, body }: { termId: string; body: TermData }) =>
      unwrap(api.PUT('/api/v1/terms/{termId}', { params: { path: { termId } }, body })),
    onSuccess: () => refreshTimetable(queryClient),
    onError: onTimetableError(queryClient),
  });

/** Deletes a term (office); one with classes is a 409 `in-use`. */
export const deleteTermMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (termId: string) =>
      expectOk(api.DELETE('/api/v1/terms/{termId}', { params: { path: { termId } } })),
    onSuccess: () => refreshTimetable(queryClient),
    onError: onTimetableError(queryClient),
  });

/** A member of a class, with the date they joined if known. */
export type ClassMember = components['schemas']['ClassMember'];
/** A person to enroll in a class; no `since` means unknown. */
export type ClassMemberData = PostBody<'/api/v1/classes/{classId}/members'>;

/** The members of a class (office), ordered by family and given name. */
export const classMembersQuery = (classId: string) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'classes', classId, 'members'],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.GET('/api/v1/admin/classes/{classId}/members', {
            params: { path: { classId } },
            signal,
          }),
        )
      ).items,
  });

/**
 * After a write to the members of a class, whatever the answer: a refused
 * write (a 409 for a member already there, a 404 for one gone) means the
 * list is out of date, and a 404 may mean the class itself is gone.
 */
function onMembersSettled(queryClient: QueryClient) {
  return (_data: unknown, error: Error | null, { classId }: { classId: string }) => {
    if (error) onTimetableError(queryClient)(error);
    // Members are booked into the coming lessons of the class, and their
    // bookings canceled when they leave (ADR 0009). Not awaited: the
    // members list must settle first, or the sheet's own callbacks (toasts)
    // would wait for every schedule query.
    if (!error) void refreshLessons(queryClient);
    return queryClient.invalidateQueries({ queryKey: classMembersQuery(classId).queryKey });
  };
}

/**
 * Adds a person to a class (office) and books them into its coming
 * lessons; a member already there is a 409 `duplicate`, a class whose
 * members fill its capacity a 409 `full`, an unknown person a 422 on
 * `body.personId`, a retired class a 422 on `path.classId`.
 */
export const addClassMemberMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ classId, body }: { classId: string; body: ClassMemberData }) =>
      unwrap(
        api.POST('/api/v1/classes/{classId}/members', { params: { path: { classId } }, body }),
      ),
    onSettled: onMembersSettled(queryClient),
  });

/** New dates of a membership (updateClassMember); no `pausedUntil` ends a pause. */
export type ClassMemberUpdateData = PutBody<'/api/v1/classes/{classId}/members/{personId}'>;

/**
 * Replaces the dates of a membership (office): `since`, and `pausedUntil`
 * (left out: ends a pause). The member is booked into the coming lessons
 * they are active on and have no booking for; one who is not a member is a
 * 404.
 */
export const updateClassMemberMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({
      classId,
      personId,
      body,
    }: {
      classId: string;
      personId: string;
      body: ClassMemberUpdateData;
    }) =>
      unwrap(
        api.PUT('/api/v1/classes/{classId}/members/{personId}', {
          params: { path: { classId, personId } },
          body,
        }),
      ),
    onSettled: onMembersSettled(queryClient),
  });

/**
 * Ends a person's membership in a class (office) and cancels their bookings
 * of its coming lessons; one who is not a member is a 404.
 */
export const removeClassMemberMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ classId, personId }: { classId: string; personId: string }) =>
      expectOk(
        api.DELETE('/api/v1/classes/{classId}/members/{personId}', {
          params: { path: { classId, personId } },
        }),
      ),
    onSettled: onMembersSettled(queryClient),
  });

/** The editable fields of a lesson (updateLesson); no `accompanistId` means none. */
export type LessonData = PutBody<'/api/v1/lessons/{lessonId}'>;
/** A new one-off lesson: the fields of a lesson and its course type. */
export type LessonCreateData = PostBody<'/api/v1/lessons'>;
/** The optional note of a cancellation, for those booked. */
export type LessonCancelData = PostBody<'/api/v1/lessons/{lessonId}/cancel'>;
/** A restore; `confirmRoomOverlap` restores even if another lesson took the room meanwhile. */
export type LessonRestoreData = components['schemas']['LessonRestoreData'];

/**
 * After a write to a lesson, a booking or the names of a family: the
 * schedule, the instructor view, "My lessons", the family's and the
 * office's passes (a booking may use or give back a visit) and the lessons
 * are asked again, but for `except` (a deleted lesson, which would only be found
 * gone), and so is the demo mailbox, as a write may mail those booked. The
 * history of "My lessons" (every page loaded) only with `history`, for
 * writes that can change the past; a member's booking writes cannot.
 */
function refreshLessons(
  queryClient: QueryClient,
  { except, history = true }: { except?: string; history?: boolean } = {},
) {
  const gone = except === undefined ? undefined : hashKey(lessonQuery(except).queryKey);
  return Promise.all(
    [
      scheduleQueryKey,
      teachQueryKey,
      history ? myLessonsQueryKey : myLessonsQuery.queryKey,
      myPassesQueryKey,
      personPassesKey,
      ['lesson'],
      demoMailboxQuery.queryKey,
    ].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey, predicate: (query) => query.queryHash !== gone }),
    ),
  );
}

/**
 * Whether a write was refused because the lesson has started: a 422 at the
 * path parameter that names the lesson (`path.lessonId`, for lesson writes)
 * or its booking (`path.bookingId`, for booking writes).
 */
export function refusedAsStarted(
  error: unknown,
  location: 'path.lessonId' | 'path.bookingId',
): boolean {
  return refusedAt(error, location);
}

/**
 * Whether a request was refused with a 422 at a location: a path parameter,
 * i.e. the record the path names, such as a retired class (`path.classId`),
 * or a query parameter or a body field.
 */
export function refusedAt(
  error: unknown,
  location: `path.${string}` | `query.${string}` | `body.${string}`,
): boolean {
  if (!(error instanceof ApiError) || error.status !== 422) return false;
  const body = error.body as { errors?: { location?: string }[] | null } | undefined;
  return (body?.errors ?? []).some((e) => e.location === location);
}

/**
 * The errors of a lesson write. A 404 means the lesson is gone, and a 422
 * at `path.lessonId` that it has started meanwhile: either way, the lessons
 * are asked again.
 */
function onLessonError(queryClient: QueryClient) {
  return (error: Error) => {
    const gone = error instanceof ApiError && error.status === 404;
    if (gone || refusedAsStarted(error, 'path.lessonId')) void refreshLessons(queryClient);
  };
}

/**
 * Adds a one-off lesson (office). A start in the past is a 409 `past`
 * unless `confirmPast` is set; another lesson in the room at an overlapping
 * time a 409 `room-taken` naming it, unless `confirmRoomOverlap` is set.
 */
export const createLessonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: LessonCreateData) => unwrap(api.POST('/api/v1/lessons', { body })),
    onSuccess: () => refreshLessons(queryClient),
    onError: onLessonError(queryClient),
  });

/**
 * Replaces the time, room, staff, capacity, visibility and kind of a lesson
 * that has not started (office; a 422 otherwise); a lesson of a class is
 * detached from it. Hiding and unhiding a lesson are this write with another
 * visibility. A new start or room mails those booked; a move into the past
 * and a taken room are refused like in createLessonMutation.
 */
export const updateLessonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ lessonId, body }: { lessonId: string; body: LessonData }) =>
      unwrap(api.PUT('/api/v1/lessons/{lessonId}', { params: { path: { lessonId } }, body })),
    onSuccess: () => refreshLessons(queryClient),
    onError: onLessonError(queryClient),
  });

/** Cancels a lesson that has not started (office) and mails those booked, with the note. */
export const cancelLessonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ lessonId, body }: { lessonId: string; body: LessonCancelData }) =>
      unwrap(
        api.POST('/api/v1/lessons/{lessonId}/cancel', { params: { path: { lessonId } }, body }),
      ),
    onSuccess: () => refreshLessons(queryClient),
    onError: onLessonError(queryClient),
  });

/**
 * Lets a canceled lesson take place after all (office) and mails those
 * booked. Another lesson in its room meanwhile is a 409 `room-taken`, unless
 * `confirmRoomOverlap` is set.
 */
export const restoreLessonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ lessonId, body }: { lessonId: string; body: LessonRestoreData }) =>
      unwrap(
        api.POST('/api/v1/lessons/{lessonId}/restore', { params: { path: { lessonId } }, body }),
      ),
    onSuccess: () => refreshLessons(queryClient),
    onError: onLessonError(queryClient),
  });

/**
 * Deletes a one-off lesson nobody has booked (office); a booked one is a 409
 * `in-use`, a lesson of a class a 409 `generated` (it is canceled instead).
 */
export const deleteLessonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (lessonId: string) =>
      expectOk(api.DELETE('/api/v1/lessons/{lessonId}', { params: { path: { lessonId } } })),
    onSuccess: (_: void, lessonId: string) => refreshLessons(queryClient, { except: lessonId }),
    onError: onLessonError(queryClient),
  });

/** A clone of a school day or a Monday-based week to another (previewLessonClone, cloneLessons). */
export type LessonCloneData = PostBody<'/api/v1/admin/lessons/clone'>;
/** What a clone creates (or would create, in a preview) and the source lessons it skips. */
export type LessonClone = components['schemas']['LessonClone'];

/**
 * What cloneLessonsMutation would create and skip now, without changing
 * anything (office). The answer may be stale by the time the clone runs.
 */
export const previewLessonCloneMutation = mutationOptions({
  mutationFn: (body: LessonCloneData) =>
    unwrap(api.POST('/api/v1/admin/lessons/clone/preview', { body })),
});

/**
 * Copies the scheduled lessons of a day or a week as one-off lessons
 * (office); the answer lists what it really created and skipped. Nobody is
 * mailed, but the schedule and the lessons are asked again.
 */
export const cloneLessonsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: LessonCloneData) =>
      unwrap(api.POST('/api/v1/admin/lessons/clone', { body })),
    onSuccess: () => refreshLessons(queryClient),
  });

/** A booking of a lesson, as bookLesson and cancelBooking answer. */
export type Booking = components['schemas']['Booking'];
/** A person of the signed-in family with an active booking for a lesson. */
export type BookedFor = components['schemas']['BookedFor'];
/**
 * The bookings cancelFollowing cancels (or would cancel), the requested one
 * first, up to `until`, and the member's pause (`pausedUntil`), if any.
 */
export type FollowingCancellation = components['schemas']['FollowingCancellation'];

/** What the schedule and the lesson queries know of the bookings of a lesson. */
type Availability = Pick<ScheduleLesson, 'capacity' | 'freePlaces' | 'bookedFor'>;
type AvailabilityPatch = <L extends Availability>(lesson: L) => L;

/**
 * The key of every booking write. While one is running, the others do not
 * refresh the lessons when they settle (a refetch could drop its optimistic
 * change); the last one refreshes them.
 */
const bookingMutationKey = ['bookings', 'write'] as const;

/**
 * Changes what the schedule and the lesson query show of a lesson's
 * bookings, in every cached copy, e.g. before the API answers. Running
 * fetches are canceled first, so they cannot overwrite the change.
 */
async function patchAvailability(
  queryClient: QueryClient,
  lessonId: string,
  patch: AvailabilityPatch,
): Promise<void> {
  const lessonKey = lessonQuery(lessonId).queryKey;
  await Promise.all(
    [scheduleQueryKey, lessonKey].map((queryKey) => queryClient.cancelQueries({ queryKey })),
  );
  applyAvailability(queryClient, lessonId, patch);
}

/** Like patchAvailability, but at once, e.g. to undo an optimistic change. */
function applyAvailability(queryClient: QueryClient, lessonId: string, patch: AvailabilityPatch) {
  queryClient.setQueriesData<Schedule>(
    { queryKey: scheduleQueryKey },
    (schedule) =>
      schedule && {
        ...schedule,
        lessons: schedule.lessons.map((l) => (l.id === lessonId ? patch(l) : l)),
      },
  );
  queryClient.setQueryData(lessonQuery(lessonId).queryKey, (lesson) => lesson && patch(lesson));
}

/** The lesson's bookings without those `drop` matches (the key is left out once none are left). */
function withoutBooked<L extends Availability>(
  lesson: L,
  drop: (b: BookedFor) => boolean,
  freePlaces: number,
): L {
  const { bookedFor = [], ...rest } = lesson;
  const left = bookedFor.filter((b) => !drop(b));
  return (
    left.length > 0 ? { ...rest, freePlaces, bookedFor: left } : { ...rest, freePlaces }
  ) as L;
}

/**
 * The optimistic booking of a person: one place fewer and the person
 * booked under the empty booking ID (unknown until the API answers). A
 * lesson that is full or has the person booked already stays as it is.
 */
const addPending =
  (personId: string, name: string): AvailabilityPatch =>
  (lesson) =>
    lesson.freePlaces === 0 || lesson.bookedFor?.some((b) => b.personId === personId)
      ? lesson
      : {
          ...lesson,
          freePlaces: lesson.freePlaces - 1,
          bookedFor: [...(lesson.bookedFor ?? []), { personId, name, bookingId: '' }],
        };

/** Undoes addPending, and only it: the pending booking of the person goes, its place is free. */
const removePending =
  (personId: string): AvailabilityPatch =>
  (lesson) =>
    lesson.bookedFor?.some((b) => b.personId === personId && b.bookingId === '')
      ? withoutBooked(
          lesson,
          (b) => b.personId === personId && b.bookingId === '',
          Math.min(lesson.capacity, lesson.freePlaces + 1),
        )
      : lesson;

/** The booking `bookingId` gone, and its place free again. */
const removeBooked =
  (bookingId: string): AvailabilityPatch =>
  (lesson) =>
    lesson.bookedFor?.some((b) => b.bookingId === bookingId)
      ? withoutBooked(
          lesson,
          (b) => b.bookingId === bookingId,
          Math.min(lesson.capacity, lesson.freePlaces + 1),
        )
      : lesson;

/** Undoes removeBooked, and only it: `entry` is booked again and takes its place back. */
const restoreBooked =
  (entry: BookedFor): AvailabilityPatch =>
  (lesson) =>
    lesson.bookedFor?.some((b) => b.personId === entry.personId)
      ? lesson
      : {
          ...lesson,
          freePlaces: Math.max(0, lesson.freePlaces - 1),
          bookedFor: [...(lesson.bookedFor ?? []), entry],
        };

/**
 * The lessons are asked again once no other booking write is running; the
 * history of "My lessons" only with `history` (staff writes such as
 * attendance can change it, a member's cannot).
 */
function refreshAfterBooking(queryClient: QueryClient, { history = false } = {}) {
  // The settling mutation itself still counts.
  if (queryClient.isMutating({ mutationKey: bookingMutationKey }) > 1) return;
  return refreshLessons(queryClient, { history });
}

/** Whom to book for which lesson; `name` shows the person as booked until the API answers. */
export type BookLessonVariables = { lessonId: string; personId: string; name: string };

/**
 * Books a lesson for the signed-in person or one of their dependents. The
 * schedule and the lesson show the booking at once; if the API refuses it
 * (409 `already-booked`, `full` or `closed` with a `why`, 404 for a lesson
 * or person out of reach), exactly that change is undone, so other booking
 * writes running at the same time keep theirs. Afterwards the lessons and
 * the demo mailbox (the confirmation mail) are asked again.
 */
export const bookLessonMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ lessonId, personId }: BookLessonVariables) =>
      unwrap(
        api.POST('/api/v1/lessons/{lessonId}/bookings', {
          params: { path: { lessonId } },
          body: { personId },
        }),
      ),
    onMutate: ({ lessonId, personId, name }: BookLessonVariables) =>
      patchAvailability(queryClient, lessonId, addPending(personId, name)),
    onError: (_error: Error, { lessonId, personId }: BookLessonVariables) => {
      applyAvailability(queryClient, lessonId, removePending(personId));
    },
    onSettled: () => refreshAfterBooking(queryClient),
  });

/** Which booking to cancel, and its lesson (for the optimistic update). */
export type CancelBookingVariables = { bookingId: string; lessonId: string };

/** The cached entry of a booking, from the lesson query or any schedule. */
function cachedBooking(
  queryClient: QueryClient,
  { bookingId, lessonId }: CancelBookingVariables,
): BookedFor | undefined {
  const lessons = [
    queryClient.getQueryData(lessonQuery(lessonId).queryKey),
    ...queryClient
      .getQueriesData<Schedule>({ queryKey: scheduleQueryKey })
      .flatMap(([, schedule]) => schedule?.lessons.filter((l) => l.id === lessonId) ?? []),
  ];
  return lessons.flatMap((l) => l?.bookedFor ?? []).find((b) => b.bookingId === bookingId);
}

/**
 * Cancels a booking of the signed-in person or one of their dependents; the
 * answer's `late` says whether it came after the cancellation cutoff. The
 * place shows free at once; if the API refuses (409 `already-canceled`, a
 * 422 on `path.bookingId` once the lesson has started, 404), exactly that
 * change is undone. Afterwards the lessons and the demo mailbox are asked
 * again.
 */
export const cancelBookingMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ bookingId }: CancelBookingVariables) =>
      unwrap(api.POST('/api/v1/bookings/{bookingId}/cancel', { params: { path: { bookingId } } })),
    onMutate: async (variables: CancelBookingVariables) => {
      const entry = cachedBooking(queryClient, variables);
      await patchAvailability(queryClient, variables.lessonId, removeBooked(variables.bookingId));
      return { entry };
    },
    onError: (
      _error: Error,
      { lessonId }: CancelBookingVariables,
      context: { entry?: BookedFor } | undefined,
    ) => {
      if (context?.entry) applyAvailability(queryClient, lessonId, restoreBooked(context.entry));
    },
    onSettled: () => refreshAfterBooking(queryClient),
  });

/**
 * What cancelFollowingMutation would cancel now: the booking of a class
 * lesson and the same person's later bookings of that class up to the
 * school date `until` that have not started, each with its own `late` flag.
 * Without `until` the answer names the default (the end of the term) as
 * `until`; for a member of the class it names the pause (`pausedUntil`).
 * Always asked anew.
 */
export const cancelFollowingPreviewQuery = (bookingId: string, until?: string) =>
  queryOptions({
    queryKey: ['bookings', bookingId, 'cancel-following', until ?? 'default'],
    queryFn: ({ signal }) =>
      unwrap(
        api.GET('/api/v1/bookings/{bookingId}/cancel-following', {
          params: { path: { bookingId }, query: until ? { until } : {} },
          signal,
        }),
      ),
    staleTime: 0,
    gcTime: 0,
  });

/**
 * Cancels a booking of a class lesson and the same person's later bookings
 * of that class up to `until` (default: the end of the term) in one go, and
 * pauses a member of the class until then; the answer lists what it
 * canceled. The account gets one mail. Afterwards the lessons and the demo
 * mailbox are asked again.
 */
export const cancelFollowingMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ bookingId, until }: { bookingId: string; until?: string }) =>
      unwrap(
        api.POST('/api/v1/bookings/{bookingId}/cancel-following', {
          params: { path: { bookingId } },
          body: until ? { until } : {},
        }),
      ),
    mutationKey: bookingMutationKey,
    onSettled: () => refreshAfterBooking(queryClient),
  });

/** An entry of a lesson's waiting list, as joinWaitlist and declineWaitlistInvite answer. */
export type WaitlistEntry = components['schemas']['WaitlistEntry'];
/** A person of the signed-in family on a lesson's waiting list, as the schedule and the lesson show them. */
export type WaitlistFor = components['schemas']['WaitlistFor'];
/**
 * What the link of a waiting-list invite stands for (getWaitlistInvite);
 * state `waiting`: the invite ran out, but the entry is back on the list,
 * at `position`.
 */
export type WaitlistInvite = components['schemas']['WaitlistInvite'];

/** Who joins which lesson's waiting list; `name` is for the messages. */
export type JoinWaitlistVariables = {
  lessonId: string;
  personId: string;
  name: string;
  stayOnList: boolean;
};

/**
 * Puts the signed-in person or a dependent on the waiting list of a full
 * lesson (409 `not-full`, `already-waiting`, `already-booked`, or `closed`
 * with a `why`; 404 for a lesson or person out of reach). No optimistic
 * change: afterwards the lessons and the demo mailbox are asked again.
 */
export const joinWaitlistMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ lessonId, personId, stayOnList }: JoinWaitlistVariables) =>
      unwrap(
        api.POST('/api/v1/lessons/{lessonId}/waitlist', {
          params: { path: { lessonId } },
          body: { personId, stayOnList },
        }),
      ),
    onSettled: () => refreshAfterBooking(queryClient),
  });

/** A waiting-list entry of the family to act on; `name` is for the messages. */
export type WaitlistEntryVariables = { entryId: string; name: string };

/**
 * Takes an entry of the family off its waiting list (409 `not-waiting` once
 * it ended); a place its invite held goes to the next. Afterwards the
 * lessons and the demo mailbox are asked again.
 */
export const leaveWaitlistMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ entryId }: WaitlistEntryVariables) =>
      expectOk(api.DELETE('/api/v1/waitlist/{entryId}', { params: { path: { entryId } } })),
    onSettled: () => refreshAfterBooking(queryClient),
  });

/**
 * Takes the place an invite of the family holds: books the person and
 * returns the booking (409 `not-invited`, `invite-expired`, `invite-used`,
 * or `closed` with a `why`). Afterwards the lessons and the demo mailbox
 * (the confirmation) are asked again.
 */
export const acceptWaitlistInviteMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ entryId }: WaitlistEntryVariables) =>
      unwrap(api.POST('/api/v1/waitlist/{entryId}/accept', { params: { path: { entryId } } })),
    onSettled: () => refreshAfterBooking(queryClient),
  });

/**
 * Gives up the place an invite of the family holds; the answer is the entry
 * as it is now (back at the end of the list with `stayOnList`, else
 * expired). Fails like acceptWaitlistInviteMutation. Afterwards the lessons
 * and the demo mailbox are asked again.
 */
export const declineWaitlistInviteMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ entryId }: WaitlistEntryVariables) =>
      unwrap(api.POST('/api/v1/waitlist/{entryId}/decline', { params: { path: { entryId } } })),
    onSettled: () => refreshAfterBooking(queryClient),
  });

/**
 * The invite of a mailed waiting-list link: the lesson, the person's given
 * name, the expiry and its state. No sign-in needed; it changes nothing. An
 * unknown token is a 404, too many lookups a 429 (neither is asked again).
 * Asked once per page: not again on focus, reconnect or retry.
 */
export const waitlistInviteQuery = (token: string) =>
  queryOptions({
    queryKey: ['waitlist-invite', token],
    queryFn: ({ signal }) =>
      unwrap(api.GET('/api/v1/waitlist-invites/{token}', { params: { path: { token } }, signal })),
    // Each lookup counts against the link's rate limit: ask once, keep the
    // answer (an accept replaces it, a refused one asks again).
    retry: false,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });

/**
 * Takes the place of a mailed invite without a sign-in, the token being the
 * proof; the answer is the invite as it is now (state `accepted`), which
 * replaces the cached one. A 409 (`invite-expired`, `invite-used`, `closed`)
 * asks for the invite again, which then says what happened. Afterwards the
 * lessons and the demo mailbox are asked again.
 */
export const acceptWaitlistInviteByTokenMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (token: string) =>
      unwrap(api.POST('/api/v1/waitlist-invites/{token}', { params: { path: { token } } })),
    onSuccess: (invite: WaitlistInvite, token: string) =>
      queryClient.setQueryData(waitlistInviteQuery(token).queryKey, invite),
    onError: (error: Error, token: string) => {
      if (conflictReason(error)) {
        void queryClient.invalidateQueries({ queryKey: waitlistInviteQuery(token).queryKey });
      }
    },
    onSettled: () => refreshLessons(queryClient, { history: false }),
  });

/** A lesson's participants as staff see them (listLessonParticipants). */
export type LessonParticipants = components['schemas']['LessonParticipants'];
/** A booking of a lesson in its participant list, canceled ones included. */
export type LessonParticipant = components['schemas']['LessonParticipant'];
/** An entry of a lesson's waiting list in its participant list, ended ones included. */
export type LessonWaitlistEntry = components['schemas']['LessonWaitlistEntry'];
/** A booking as the staff writes answer it. */
export type StaffBooking = components['schemas']['StaffBooking'];
/** Whom staff add to a lesson: `personId` or `guest`, optionally as a trial. */
export type AddParticipantData = PostBody<'/api/v1/lessons/{lessonId}/participants'>;
/** The trial flag and note of a booking. */
export type UpdateParticipantData = PutBody<'/api/v1/bookings/{bookingId}/participant'>;
/** The attendance of a booking; `booked` sets it back to unknown. */
export type AttendanceStatus = PostBody<'/api/v1/bookings/{bookingId}/attendance'>['status'];
/** An event of a booking's history (listBookingEvents). */
export type BookingEvent = components['schemas']['BookingEvent'];
/** A person found by the staff's person search: name and birth year only. */
export type StaffPersonMatch = components['schemas']['StaffPersonMatch'];

/**
 * The participants of a lesson for staff: its bookings (canceled ones
 * included), its waiting list and the counts. The office sees every lesson;
 * instructors and accompanists only those they teach, any other is a 404
 * (not asked again). Under the lesson's key, so every write to lessons or
 * bookings asks it again.
 */
export const lessonParticipantsQuery = (lessonId: string) =>
  queryOptions({
    queryKey: ['lesson', lessonId, 'participants'],
    queryFn: ({ signal }) =>
      unwrap(
        api.GET('/api/v1/lessons/{lessonId}/participants', {
          params: { path: { lessonId } },
          signal,
        }),
      ),
  });

/**
 * The history of a booking, oldest first (office): status, flags and who
 * made each change. Always asked anew when shown.
 */
export const bookingEventsQuery = (bookingId: string) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'bookings', bookingId, 'events'],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.GET('/api/v1/admin/bookings/{bookingId}/events', {
            params: { path: { bookingId } },
            signal,
          }),
        )
      ).items,
    staleTime: 0,
  });

/** The fewest characters the staff's person search needs. */
export const minStaffPersonQuery = 2;
/** The longest search the staff's person search takes, in characters (the API's maxLength). */
export const maxStaffPersonQuery = 100;
/** The most persons the staff's person search returns (`service` caps it the same). */
export const maxStaffPersonMatches = 20;

/** `q` as the staff's person search takes it: trimmed, at most 100 characters (code points). */
const staffPersonSearch = (q: string) => [...q.trim()].slice(0, maxStaffPersonQuery).join('');

/**
 * The persons whose given or family name contains every word of `q` (staff;
 * at least two characters, longer ones cut at 100), at most 20, ordered by
 * family and given name, with their birth year, no contact data.
 */
export const staffPersonsQuery = (q: string) =>
  queryOptions({
    queryKey: ['staff-persons', staffPersonSearch(q)],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.POST('/api/v1/staff/persons/search', {
            body: { query: staffPersonSearch(q) },
            signal,
          }),
        )
      ).items,
    staleTime: 30_000,
  });

/**
 * After a staff write to a lesson's participants: the lessons (their
 * participants included) and the demo mailbox, once no other booking write
 * runs, and the booking histories the office may have open.
 */
function refreshAfterParticipants(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: [...adminQueryKey, 'bookings'] });
  return refreshAfterBooking(queryClient, { history: true });
}

/** Whom to add to which lesson. */
export type AddParticipantVariables = { lessonId: string; body: AddParticipantData };

/**
 * Adds a person or a guest to a lesson (staff), as a trial if asked: 409
 * `already-booked`, `full` (unless `confirmOverCapacity`), `closed` with why
 * `canceled`; a 422 on `path.lessonId` once an instructor's day is over, or
 * on the body's fields. Afterwards the lessons and the demo mailbox (the
 * confirmation) are asked again.
 */
export const addLessonParticipantMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ lessonId, body }: AddParticipantVariables) =>
      unwrap(
        api.POST('/api/v1/lessons/{lessonId}/participants', {
          params: { path: { lessonId } },
          body,
        }),
      ),
    onSettled: () => refreshAfterParticipants(queryClient),
  });

/**
 * Sets whether an active booking is a trial and its note (staff); a
 * canceled one is a 409 `already-canceled`.
 */
export const updateLessonParticipantMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ bookingId, body }: { bookingId: string; body: UpdateParticipantData }) =>
      unwrap(
        api.PUT('/api/v1/bookings/{bookingId}/participant', {
          params: { path: { bookingId } },
          body,
        }),
      ),
    onSettled: () => refreshAfterParticipants(queryClient),
  });

/**
 * Takes a participant off a lesson (staff): cancels the booking, never
 * late; the place goes to the waiting list. A canceled booking is a 409
 * `already-canceled`.
 */
export const removeLessonParticipantMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ bookingId }: { bookingId: string }) =>
      unwrap(api.POST('/api/v1/bookings/{bookingId}/remove', { params: { path: { bookingId } } })),
    onSettled: () => refreshAfterParticipants(queryClient),
  });

/**
 * Records whether a participant came (staff), from the lesson's start: a
 * 422 on `path.bookingId` before it or once an instructor's day is over.
 */
export const setAttendanceMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: ({ bookingId, status }: { bookingId: string; status: AttendanceStatus }) =>
      unwrap(
        api.POST('/api/v1/bookings/{bookingId}/attendance', {
          params: { path: { bookingId } },
          body: { status },
        }),
      ),
    onSettled: () => refreshAfterParticipants(queryClient),
  });

/** Marks every participant whose attendance is unknown as attended; returns those it changed. */
export const markAllAttendedMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationKey: bookingMutationKey,
    mutationFn: async ({ lessonId }: { lessonId: string }) =>
      (
        await unwrap(
          api.POST('/api/v1/lessons/{lessonId}/attendance/all', {
            params: { path: { lessonId } },
          }),
        )
      ).items,
    onSettled: () => refreshAfterParticipants(queryClient),
  });

/** The longest message to a lesson's participants, in characters (code points, as the API counts). */
export const maxNoticeMessage = 1000;

/** What to tell whom: the lesson and the message (plain text). */
export type NotifyParticipantsVariables = { lessonId: string; message: string };

/**
 * Mails a message to the participants of a lesson (staff; notifyParticipants)
 * and returns to how many addresses it went (0: nobody could be reached). A
 * 422 on `body.message` for a message the API refuses, a 429 with
 * Retry-After beyond the limits (5 an hour per account, 3 a school day per
 * lesson), a 404 for a lesson the caller does not teach. Afterwards the demo
 * mailbox is asked again.
 */
export const notifyParticipantsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: async ({ lessonId, message }: NotifyParticipantsVariables) =>
      (
        await unwrap(
          api.POST('/api/v1/lessons/{lessonId}/notify', {
            params: { path: { lessonId } },
            body: { message },
          }),
        )
      ).recipients,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey }),
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

/** An active calendar feed of the signed-in account (never its token). */
export type CalendarFeed = components['schemas']['CalendarFeed'];
/** What a calendar feed shows: the family's bookings, the own teaching or the schedule. */
export type CalendarFeedScope = CalendarFeed['scope'];
/** A feed just created, with its secret token and link (shown only once). */
export type NewCalendarFeed = components['schemas']['NewCalendarFeed'];
/** What a new feed shows: its scope and, for the schedule of one location, the location. */
export type CalendarFeedSubject = PostBody<'/api/v1/my/calendar-feeds'>;

/** The active calendar feeds of the signed-in account, oldest first (listCalendarFeeds). */
export const calendarFeedsQuery = queryOptions({
  queryKey: ['calendar-feeds'],
  queryFn: async ({ signal }) =>
    (await unwrap(api.GET('/api/v1/my/calendar-feeds', { signal }))).items,
});

const refreshCalendarFeeds = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: calendarFeedsQuery.queryKey });

/**
 * Creates the link of a calendar feed (createCalendarFeed), which revokes
 * the account's active link of the same subject. The answer carries the
 * token and the link, which cannot be shown again. A 403 for a teaching
 * feed of an account without a staff record of its own, and for any feed
 * while an admin is signed in as the account.
 */
export const createCalendarFeedMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: CalendarFeedSubject) =>
      unwrap(api.POST('/api/v1/my/calendar-feeds', { body })),
    onSuccess: () => refreshCalendarFeeds(queryClient),
  });

/**
 * Revokes an active calendar feed of the signed-in account
 * (revokeCalendarFeed): its link stops working. A 404 means it was revoked
 * or replaced meanwhile; either way the list is asked again.
 */
export const revokeCalendarFeedMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (feedId: string) =>
      expectOk(api.DELETE('/api/v1/my/calendar-feeds/{feedId}', { params: { path: { feedId } } })),
    onSettled: () => refreshCalendarFeeds(queryClient),
  });

/** Whether the installation sends web push notifications, and the VAPID key to subscribe with (ADR 0017). */
export type PushStatus = components['schemas']['PushStatus'];

/** Prefix of the web push query keys. */
export const pushQueryKey = ['push'] as const;

/** Whether push is on (getPushStatus); it changes only with the server's configuration. */
export const pushStatusQuery = queryOptions({
  queryKey: [...pushQueryKey, 'status'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/my/push', { signal })),
  staleTime: Infinity,
});

/** This device's notifications for an account: on, and what the browser allows. */
export interface PushDeviceState {
  on: boolean;
  permission: NotificationPermission;
}

/**
 * Whether this device gets the account's notifications: it has a
 * subscription, and this account turned it on (pushOwner).
 */
export const pushDeviceQuery = (accountId: string) =>
  queryOptions({
    queryKey: [...pushQueryKey, 'device', accountId],
    queryFn: async (): Promise<PushDeviceState> => ({
      on: pushOwner() === accountId && (await pushDevice.current()) !== null,
      permission: pushDevice.permission(),
    }),
  });

/** The browser refused the permission to show notifications. */
export class NotificationsBlockedError extends Error {
  constructor() {
    super('Notifications are blocked');
    this.name = 'NotificationsBlockedError';
  }
}

/** Saves this device's subscription for the signed-in account (savePushSubscription). */
const savePushSubscription = (sub: DeviceSubscription) =>
  expectOk(
    api.POST('/api/v1/my/push/subscriptions', { body: { ...sub, deviceLabel: deviceLabel() } }),
  );

/**
 * Turns notifications on for this device: asks the browser's permission,
 * subscribes (or takes the device's subscription over from an account that
 * signed in here before) and saves it for the account.
 */
export const enablePushMutation = (queryClient: QueryClient, accountId: string) =>
  mutationOptions({
    mutationFn: async (publicKey: string | undefined) => {
      if ((await pushDevice.requestPermission()) !== 'granted')
        throw new NotificationsBlockedError();
      const sub = (await pushDevice.current()) ?? (await pushDevice.subscribe(publicKey));
      await savePushSubscription(sub);
      setPushOwner(accountId);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: [...pushQueryKey, 'device'] }),
  });

/** Turns notifications off for this device: the server forgets it, then the browser. */
export const disablePushMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: async () => {
      const sub = await pushDevice.current();
      if (sub) {
        await expectOk(
          api.DELETE('/api/v1/my/push/subscriptions', { body: { endpoint: sub.endpoint } }),
        );
        await pushDevice.unsubscribe();
      }
      setPushOwner(null);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: [...pushQueryKey, 'device'] }),
  });

/**
 * Saves this device's subscription again if the account turned it on, so
 * the server sees it is still in use (it drops subscriptions unused for six
 * months). Failures are ignored: it is tried again next time.
 */
export async function refreshPushSubscription(accountId: string): Promise<void> {
  if (pushOwner() !== accountId) return;
  try {
    const sub = await pushDevice.current();
    if (sub) await savePushSubscription(sub);
  } catch {
    // Offline or push off: next time.
  }
}

/**
 * Before a sign-out: the device stops getting notifications (ADR 0017).
 * Best effort; the sign-out goes on whatever happens here.
 */
async function forgetPushDevice(): Promise<void> {
  if (!pushOwner()) return;
  try {
    const sub = await pushDevice.current();
    if (sub) {
      await api.DELETE('/api/v1/my/push/subscriptions', { body: { endpoint: sub.endpoint } });
      await pushDevice.unsubscribe();
    }
  } catch {
    // The push service drops a subscription the server keeps once the
    // browser unsubscribed; the server then deletes it (410).
  } finally {
    setPushOwner(null);
  }
}

/** A pass type of the school's catalogue (office). */
export type PassType = components['schemas']['PassType'];
/** The editable fields of a pass type (create and update). */
export type PassTypeData = PostBody<'/api/v1/admin/pass-types'>;
/** A pass as the office sees it: with the office's note. */
export type AdminPass = components['schemas']['AdminPass'];
/** A pass of the signed-in family (listMyPasses), without the office's note. */
export type Pass = components['schemas']['Pass'];
/** A booking a pass paid for, as the pass lists it. */
export type PassUse = components['schemas']['PassUse'];
/** When bookings use a pass: on booking or on attendance. */
export type RedemptionMode = components['schemas']['PassSettings']['redemptionMode'];
/** The family's passes with the school's today, its time zone and when bookings use a pass. */
export type MyPasses = components['schemas']['MyPasses'];
/** What the office sends to sell a pass. */
export type SellPassData = PostBody<'/api/v1/admin/persons/{personId}/passes/sell'>;
/** What the office sends to give a pass away. */
export type AssignPassData = PostBody<'/api/v1/admin/persons/{personId}/passes/assign'>;
/** How a booking is paid: by a pass, unpaid, free or pending attendance. */
export type BookingPayment = components['schemas']['BookingPayment'];

/** Every pass type, retired ones included, ordered by name (office). */
export const passTypesQuery = queryOptions({
  queryKey: [...adminQueryKey, 'pass-types'],
  queryFn: async ({ signal }) =>
    (await unwrap(api.GET('/api/v1/admin/pass-types', { signal }))).items,
});

/** Prefix of the queries of a person's passes (office). */
const personPassesKey = [...adminQueryKey, 'passes'] as const;

/** The passes of a person (office): the usable ones first, then the others, newest sale first. */
export const personPassesQuery = (personId: string) =>
  queryOptions({
    queryKey: [...personPassesKey, personId],
    queryFn: async ({ signal }) =>
      (
        await unwrap(
          api.GET('/api/v1/admin/persons/{personId}/passes', {
            params: { path: { personId } },
            signal,
          }),
        )
      ).items,
  });

/** Prefix of the family's passes, asked again after every lesson or booking write. */
export const myPassesQueryKey = ['my-passes'] as const;

/**
 * The passes of the signed-in person and their dependents with what they
 * paid for (listMyPasses): usable first, then the others, newest sale first.
 */
export const myPassesQuery = queryOptions({
  queryKey: myPassesQueryKey,
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/my/passes', { signal })),
});

/** When bookings use a pass (office reads, an admin writes). */
export const passSettingsQuery = queryOptions({
  queryKey: [...adminQueryKey, 'settings', 'passes'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/admin/settings/passes', { signal })),
});

/** After a write to the pass catalogue: the pass types and the passes that name them. */
function refreshPassTypes(queryClient: QueryClient) {
  return Promise.all(
    [passTypesQuery.queryKey, personPassesKey, myPassesQueryKey].map((queryKey) =>
      queryClient.invalidateQueries({ queryKey }),
    ),
  );
}

/** The errors of a write to the pass catalogue: a 404 means it changed meanwhile. */
function onPassTypeError(queryClient: QueryClient) {
  return (error: Error) => {
    if (error instanceof ApiError && error.status === 404) void refreshPassTypes(queryClient);
  };
}

/** Adds a pass type (office); a name another one has (ignoring case) is a 409 `duplicate`. */
export const createPassTypeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: PassTypeData) => unwrap(api.POST('/api/v1/admin/pass-types', { body })),
    onSuccess: () => refreshPassTypes(queryClient),
    onError: onPassTypeError(queryClient),
  });

/**
 * Replaces the fields of a pass type (office); `active: false` retires it.
 * Passes sold already keep their visits, days and price.
 */
export const updatePassTypeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ passTypeId, body }: { passTypeId: string; body: PassTypeData }) =>
      unwrap(
        api.PUT('/api/v1/admin/pass-types/{passTypeId}', {
          params: { path: { passTypeId } },
          body,
        }),
      ),
    onSuccess: () => refreshPassTypes(queryClient),
    onError: onPassTypeError(queryClient),
  });

/**
 * Retires a pass type (office): the way out when it cannot be deleted
 * because it was sold. Only its sale stops; nothing else changes. A type
 * gone meanwhile is a 404.
 */
export const retirePassTypeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: async ({ id }: PassType) => {
      await unwrap(
        api.POST('/api/v1/admin/pass-types/{passTypeId}/retire', {
          params: { path: { passTypeId: id } },
        }),
      );
    },
    onSuccess: () => refreshPassTypes(queryClient),
    onError: onPassTypeError(queryClient),
  });

/** Deletes a pass type that was never sold (office); a sold one is a 409 `in-use`. */
export const deletePassTypeMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (passTypeId: string) =>
      expectOk(
        api.DELETE('/api/v1/admin/pass-types/{passTypeId}', { params: { path: { passTypeId } } }),
      ),
    onSuccess: () => refreshPassTypes(queryClient),
    onError: onPassTypeError(queryClient),
  });

/**
 * After a pass is sold, given or voided: the person's passes, the family's
 * passes, and whatever shows how bookings are paid (a void moves the coming
 * bookings to another pass, or leaves them unpaid); a sale also issues and
 * mails a receipt, so the receipts and the demo mailbox too.
 */
function refreshPasses(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: personPassesKey }),
    queryClient.invalidateQueries({ queryKey: receiptsKey }),
    queryClient.invalidateQueries({ queryKey: myReceiptsQuery.queryKey }),
    refreshLessons(queryClient),
  ]);
}

/** Whose pass to sell or give: the person and the request body. */
export type PassSaleVariables<B> = { personId: string; body: B };

/**
 * Sells a pass to a person (office). An unknown person is a 404; an
 * unknown or retired type a 422 on `body.passTypeId`, an unknown payer on
 * `body.payerId`.
 */
export const sellPassMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, body }: PassSaleVariables<SellPassData>) =>
      unwrap(
        api.POST('/api/v1/admin/persons/{personId}/passes/sell', {
          params: { path: { personId } },
          body,
        }),
      ),
    onSuccess: () => refreshPasses(queryClient),
  });

/** Gives a person a pass without payment (office); it fails like sellPassMutation. */
export const assignPassMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ personId, body }: PassSaleVariables<AssignPassData>) =>
      unwrap(
        api.POST('/api/v1/admin/persons/{personId}/passes/assign', {
          params: { path: { personId } },
          body,
        }),
      ),
    onSuccess: () => refreshPasses(queryClient),
  });

/**
 * Voids a pass (office): it pays for nothing more; what it paid for stays
 * paid. A voided pass is a 409 `already-voided`, after which the passes are
 * asked again too.
 */
export const voidPassMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (passId: string) =>
      unwrap(api.POST('/api/v1/admin/passes/{passId}/void', { params: { path: { passId } } })),
    onSettled: () => refreshPasses(queryClient),
  });

/**
 * Sets when bookings use a pass from now on (admin; the office gets a
 * 403). Bookings redeemed before stay as they are; what shows how bookings
 * are paid is asked again.
 */
export const updatePassSettingsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (redemptionMode: RedemptionMode) =>
      unwrap(api.PUT('/api/v1/admin/settings/passes', { body: { redemptionMode } })),
    onSuccess: (settings) => {
      queryClient.setQueryData(passSettingsQuery.queryKey, settings);
      // How bookings are paid follows the mode: pending or paid.
      return refreshLessons(queryClient);
    },
  });

/** The office's figures of a range of school dates (aggregates only). */
export type Statistics = components['schemas']['Statistics'];
/** The range of the statistics: a preset, or a custom from–to. */
export type StatisticsParams = NonNullable<
  paths['/api/v1/admin/statistics']['get']['parameters']['query']
>;

/**
 * The statistics of `params` (office). A custom range of more than 366
 * days is a 422, the term range without a current term a 404.
 */
export const statisticsQuery = (params: StatisticsParams) =>
  queryOptions({
    queryKey: [...adminQueryKey, 'statistics', params],
    queryFn: ({ signal }) =>
      unwrap(api.GET('/api/v1/admin/statistics', { params: { query: params }, signal })),
  });

/** A receipt (a payment confirmation of a pass sale, or its cancellation), as issued. */
export type Receipt = components['schemas']['Receipt'];
/** How a pass was paid, printed on its receipt. */
export type PaymentMethod = Receipt['paymentMethod'];
/** The school's tax mode, as receipts print it. */
export type TaxMode = Receipt['taxMode'];
/** The receipt settings with whether receipts can be issued, the first gap, and the logo's type. */
export type ReceiptSettings = components['schemas']['ReceiptSettingsBody'];
/** The receipt settings an admin writes. */
export type ReceiptSettingsData = components['schemas']['ReceiptSettingsData'];
/** The receipt list filter: a year (0: all), a search, and a page. */
export type ReceiptsParams = PostBody<'/api/v1/admin/receipts/search'>;

/** Prefix of the office's receipt queries. */
const receiptsKey = [...adminQueryKey, 'receipts'] as const;

/**
 * The receipts that match `params`, newest first, and how many match in all
 * (office). A POST, so that the name searched for stays out of URLs (ASVS
 * 8.3.1).
 */
export const receiptsQuery = (params: ReceiptsParams) =>
  queryOptions({
    queryKey: [...receiptsKey, 'list', params],
    queryFn: ({ signal }) =>
      unwrap(api.POST('/api/v1/admin/receipts/search', { body: params, signal })),
  });

/** One receipt (office); an unknown one is a 404. */
export const receiptQuery = (receiptId: string) =>
  queryOptions({
    queryKey: [...receiptsKey, 'one', receiptId],
    queryFn: ({ signal }) =>
      unwrap(
        api.GET('/api/v1/admin/receipts/{receiptId}', { params: { path: { receiptId } }, signal }),
      ),
  });

/** The receipts of what the signed-in person paid for, newest first. */
export const myReceiptsQuery = queryOptions({
  queryKey: ['my-receipts'],
  queryFn: async ({ signal }) => (await unwrap(api.GET('/api/v1/my/receipts', { signal }))).items,
});

/** The path of a receipt's PDF for the office, or for its payer (`mine`). */
export const receiptPdfPath = (receiptId: string, { mine = false } = {}) =>
  mine ? `/api/v1/my/receipts/${receiptId}/pdf` : `/api/v1/admin/receipts/${receiptId}/pdf`;

/** The receipt settings (office reads, an admin writes). */
export const receiptSettingsQuery = queryOptions({
  queryKey: [...adminQueryKey, 'settings', 'receipts'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/admin/settings/receipts', { signal })),
});

/** The logo printed on receipts, or null without one (office). */
export const receiptLogoQuery = queryOptions({
  queryKey: [...adminQueryKey, 'settings', 'receipts', 'logo'],
  queryFn: ({ signal }) =>
    unwrapOrNull(
      404,
      api.GET('/api/v1/admin/settings/receipts/logo', { parseAs: 'blob', signal }),
    ) as Promise<Blob | null>,
});

/**
 * Replaces the receipt settings (admin); the answer says whether receipts
 * can be issued now.
 */
export const updateReceiptSettingsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: ReceiptSettingsData) =>
      unwrap(api.PUT('/api/v1/admin/settings/receipts', { body })),
    onSuccess: (settings) => queryClient.setQueryData(receiptSettingsQuery.queryKey, settings),
  });

/** After the logo changes: the settings (their logo type), the logo, and the header's. */
const refreshLogo = (queryClient: QueryClient) =>
  Promise.all(
    [
      receiptSettingsQuery.queryKey,
      brandingSettingsQuery.queryKey,
      publicSettingsQuery.queryKey,
      schoolLogoQuery.queryKey,
    ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
  );

/**
 * Sets the logo of later receipts (admin): a PNG or JPEG of at most 512
 * KiB, base64 encoded; a larger image or data not of the type is a 422 on
 * `body.data`.
 */
export const updateReceiptLogoMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: components['schemas']['ReceiptLogoInputBody']) =>
      expectOk(api.PUT('/api/v1/admin/settings/receipts/logo', { body })),
    onSuccess: () => refreshLogo(queryClient),
  });

/** Removes the logo of later receipts (admin). */
export const deleteReceiptLogoMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: () => expectOk(api.DELETE('/api/v1/admin/settings/receipts/logo')),
    onSuccess: () => refreshLogo(queryClient),
  });

/** The school's name and legal notice (office reads, an admin writes; ADR 0013). */
export type SchoolSettings = components['schemas']['SchoolSettings'];
/** The accent color, its dark variant and whether a logo exists. */
export type BrandingSettings = components['schemas']['BrandingSettings'];
/** The accent color an admin writes. */
export type BrandingSettingsData = components['schemas']['BrandingSettingsData'];
/** The time zone, language and currency, and which of them the data fixes. */
export type RegionalSettings = components['schemas']['RegionalSettings'];
/** The time zone, language and currency an admin writes. */
export type RegionalSettingsData = components['schemas']['RegionalSettingsData'];
/** The release rule, cutoffs, invite hold and early access, with the window now. */
export type BookingRules = components['schemas']['BookingRules'];
/** The booking rules an admin writes. */
export type BookingRulesData = components['schemas']['BookingRulesData'];
/** The release window of a rule at an instant. */
export type ReleaseWindow = components['schemas']['ReleaseWindowBody'];

const settingsKey = [...adminQueryKey, 'settings'] as const;

/** After a settings write: the settings everyone sees (name, branding, zone, language). */
const refreshPublicSettings = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: publicSettingsQuery.queryKey });

export const schoolSettingsQuery = queryOptions({
  queryKey: [...settingsKey, 'school'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/admin/settings/school', { signal })),
});

/** Replaces the school's name and legal notice (admin). */
export const updateSchoolSettingsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: SchoolSettings) =>
      unwrap(api.PUT('/api/v1/admin/settings/school', { body })),
    onSuccess: (settings) => {
      queryClient.setQueryData(schoolSettingsQuery.queryKey, settings);
      return refreshPublicSettings(queryClient);
    },
  });

export const brandingSettingsQuery = queryOptions({
  queryKey: [...settingsKey, 'branding'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/admin/settings/branding', { signal })),
});

/** Sets the accent color, or the app's own (admin); too little contrast is a 422. */
export const updateBrandingSettingsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: BrandingSettingsData) =>
      unwrap(api.PUT('/api/v1/admin/settings/branding', { body })),
    onSuccess: (settings) => {
      queryClient.setQueryData(brandingSettingsQuery.queryKey, settings);
      return refreshPublicSettings(queryClient);
    },
  });

export const regionalSettingsQuery = queryOptions({
  queryKey: [...settingsKey, 'regional'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/admin/settings/regional', { signal })),
});

/**
 * Replaces the time zone, language and currency (admin). A new time zone
 * once lessons exist is a 409 `timezone-locked`, a new currency once prices
 * or receipts exist a 409 `currency-locked`; the card is asked again, as
 * what locks it appeared meanwhile.
 */
export const updateRegionalSettingsMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: RegionalSettingsData) =>
      unwrap(api.PUT('/api/v1/admin/settings/regional', { body })),
    onSuccess: (settings) => {
      queryClient.setQueryData(regionalSettingsQuery.queryKey, settings);
      return refreshPublicSettings(queryClient);
    },
    onError: (error: Error) => {
      if (conflictReason(error)) {
        void queryClient.invalidateQueries({ queryKey: regionalSettingsQuery.queryKey });
      }
    },
  });

export const bookingRulesQuery = queryOptions({
  queryKey: [...settingsKey, 'booking-rules'],
  queryFn: ({ signal }) => unwrap(api.GET('/api/v1/admin/settings/booking-rules', { signal })),
});

export type ReleasePreviewParams = NonNullable<
  paths['/api/v1/admin/settings/booking-rules/preview']['get']['parameters']['query']
>;

/** The release window a release rule would have now (office); nothing changes. */
export const releasePreviewQuery = (params: ReleasePreviewParams) =>
  queryOptions({
    queryKey: [...settingsKey, 'booking-rules', 'preview', params],
    queryFn: ({ signal }) =>
      unwrap(
        api.GET('/api/v1/admin/settings/booking-rules/preview', {
          params: { query: params },
          signal,
        }),
      ),
  });

/**
 * Replaces the booking rules from now on (admin); a wider window generates
 * its lessons at once. What is released and bookable changes, so the lesson
 * views are asked again.
 */
export const updateBookingRulesMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: BookingRulesData) =>
      unwrap(api.PUT('/api/v1/admin/settings/booking-rules', { body })),
    onSuccess: (rules) => {
      queryClient.setQueryData(bookingRulesQuery.queryKey, rules);
      return refreshLessons(queryClient);
    },
  });

/**
 * Issues the cancellation (Storno) of a sale receipt (office), which voids
 * its pass. A pass that paid for visits is a 409 `pass-used`, a receipt
 * canceled already a 409 `already-canceled`, a cancellation a 409
 * `cancellation`, incomplete settings a 409 `receipt-settings-incomplete`.
 * Afterwards the receipts, the passes, how bookings are paid and the demo
 * mailbox (the cancellation is mailed) are asked again.
 */
export const cancelReceiptMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: ({ receiptId, reason }: { receiptId: string; reason?: string }) =>
      unwrap(
        api.POST('/api/v1/admin/receipts/{receiptId}/cancel', {
          params: { path: { receiptId } },
          body: reason ? { reason } : {},
        }),
      ),
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: receiptsKey }),
        queryClient.invalidateQueries({ queryKey: myReceiptsQuery.queryKey }),
        refreshPasses(queryClient),
      ]),
  });
