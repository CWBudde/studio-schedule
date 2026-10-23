import { hashKey, mutationOptions, type QueryClient, queryOptions } from '@tanstack/react-query';
import {
  api,
  type ConfirmEmailBody,
  type LoginBody,
  type Me,
  type paths,
  type RegisterBody,
} from './client';

/** Error thrown by query functions when the API answers with a non-2xx status. */
export class ApiError extends Error {
  readonly status: number;
  readonly body: unknown;

  constructor(status: number, body: unknown) {
    super(`API request failed with status ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

/** The data of an openapi-fetch result; a non-2xx answer throws an ApiError. */
async function unwrap<T>(
  request: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await request;
  if (data === undefined) throw new ApiError(response.status, error);
  return data;
}

/** Like unwrap, but an answer with status `absent` means "nothing there": null. */
async function unwrapOrNull<T>(
  absent: number,
  request: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T | null> {
  const { data, error, response } = await request;
  if (response.status === absent) return null;
  if (data === undefined) throw new ApiError(response.status, error);
  return data;
}

/** Waits for an answer without a body (e.g. 202, 204); a non-2xx answer throws an ApiError. */
async function expectOk(request: Promise<{ error?: unknown; response: Response }>): Promise<void> {
  const { error, response } = await request;
  if (!response.ok) throw new ApiError(response.status, error);
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

/** Signs in; the account becomes the `me` query's data. */
export const loginMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: LoginBody) => unwrap(api.POST('/api/v1/auth/login', { body })),
    onSuccess: (me: Me) => changeIdentity(queryClient, me),
  });

/** Confirms an email address with the link's token and the password, and signs in. */
export const confirmEmailMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: ConfirmEmailBody) => unwrap(api.POST('/api/v1/auth/confirm', { body })),
    onSuccess: (me: Me) => changeIdentity(queryClient, me),
  });

/** Registers an account; the API mails a confirmation link (shown in the demo mailbox). */
export const registerMutation = (queryClient: QueryClient) =>
  mutationOptions({
    mutationFn: (body: RegisterBody) => expectOk(api.POST('/api/v1/auth/register', { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: demoMailboxQuery.queryKey }),
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
