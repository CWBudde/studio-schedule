import { MutationObserver, QueryClient, QueryObserver } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '@/api/client';
import i18n from '@/i18n';
import {
  accounts,
  family,
  ids,
  meAdmin,
  meImpersonated,
  meMember,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  persons,
  problem,
  schedule,
  staffEntries,
  tooManyRequests,
} from '@/test/mockApi';
import {
  addDependentMutation,
  allStaffQuery,
  ApiError,
  changePasswordMutation,
  confirmEmailChangeMutation,
  confirmEmailMutation,
  courseTypesQuery,
  createCourseTypeMutation,
  deleteStaffMutation,
  familyQuery,
  impersonateMutation,
  listAccountsQuery,
  loginMutation,
  logoutMutation,
  meQuery,
  personsQuery,
  removeDependentMutation,
  requestEmailChangeMutation,
  requestPasswordResetMutation,
  resetPasswordMutation,
  scheduleQuery,
  staffQuery,
  stopImpersonationMutation,
  updateDependentMutation,
  updateLocaleMutation,
  updateMyPersonMutation,
  updateStaffMutation,
} from './queries';

afterEach(() => {
  vi.restoreAllMocks();
});

const range = { from: '2026-10-26', to: '2026-11-09' };

/**
 * A query client with the admin signed in, a cached schedule that is not
 * shown, one that is, and the account list.
 */
async function signedInClient() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  queryClient.setQueryData(meQuery.queryKey, meAdmin);
  queryClient.setQueryData(scheduleQuery({ from: '2026-11-09', to: '2026-11-23' }).queryKey, {
    ...schedule,
  });
  queryClient.setQueryData(listAccountsQuery.queryKey, accounts);
  const shown = new QueryObserver(queryClient, scheduleQuery(range));
  const unsubscribe = shown.subscribe(() => {});
  await vi.waitFor(() => expect(shown.getCurrentResult().isSuccess).toBe(true));
  return { queryClient, shown, unsubscribe };
}

describe('identity changes', () => {
  it.each([
    {
      name: 'login',
      mutation: loginMutation,
      path: '/api/v1/auth/login',
      variables: { email: meMember.email, password: 'secret-password' },
      me: meMember,
    },
    {
      name: 'confirmEmail',
      mutation: confirmEmailMutation,
      path: '/api/v1/auth/confirm',
      variables: { token: 'tok-123', password: 'secret-password' },
      me: meMember,
    },
    {
      name: 'impersonate',
      mutation: impersonateMutation,
      path: '/api/v1/admin/accounts/{accountId}/impersonate',
      variables: meMember.accountId,
      me: meImpersonated,
    },
    {
      name: 'stopImpersonation',
      mutation: stopImpersonationMutation,
      path: '/api/v1/auth/impersonation/stop',
      variables: undefined,
      me: meAdmin,
    },
    {
      name: 'logout',
      mutation: logoutMutation,
      path: '/api/v1/auth/logout',
      variables: undefined,
      me: null,
    },
  ])(
    '$name sets me and refetches what depends on the viewer',
    async ({ mutation, path, variables, me }) => {
      let scheduleRequests = 0;
      mockApiGet({
        '/api/v1/schedule': () => {
          scheduleRequests++;
          return { body: schedule };
        },
      });
      mockApiPost({ [path]: me ? { body: me } : { status: 204 } });
      const { queryClient, shown, unsubscribe } = await signedInClient();
      expect(scheduleRequests).toBe(1);

      const observer = new MutationObserver(
        queryClient,
        mutation(queryClient) as ReturnType<typeof loginMutation>,
      );
      await observer.mutate(variables as never);

      expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(me);
      // The shown schedule is asked again; the hidden one and the admin's
      // account list are gone, so they cannot show data of the old identity.
      await vi.waitFor(() => expect(scheduleRequests).toBe(2));
      expect(shown.getCurrentResult().data).toEqual(schedule);
      expect(
        queryClient.getQueryData(scheduleQuery({ from: '2026-11-09', to: '2026-11-23' }).queryKey),
      ).toBeUndefined();
      expect(queryClient.getQueryState(listAccountsQuery.queryKey)).toBeUndefined();
      unsubscribe();
    },
  );
});

describe('stopImpersonation', () => {
  it('signs out locally when the impersonation has expired (401)', async () => {
    mockApiGet({ '/api/v1/schedule': { body: schedule } });
    mockApiPost({ '/api/v1/auth/impersonation/stop': { status: 401 } });
    const { queryClient, unsubscribe } = await signedInClient();
    queryClient.setQueryData(meQuery.queryKey, meImpersonated);

    await expect(
      new MutationObserver(queryClient, stopImpersonationMutation(queryClient)).mutate(),
    ).rejects.toThrow();

    expect(queryClient.getQueryData(meQuery.queryKey)).toBeNull();
    unsubscribe();
  });

  it('keeps the impersonation on other errors', async () => {
    mockApiPost({ '/api/v1/auth/impersonation/stop': { status: 500 } });
    const queryClient = new QueryClient();
    queryClient.setQueryData(meQuery.queryKey, meImpersonated);

    await expect(
      new MutationObserver(queryClient, stopImpersonationMutation(queryClient)).mutate(),
    ).rejects.toThrow();

    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meImpersonated);
  });
});

describe('admin queries', () => {
  it('lists the accounts', async () => {
    const get = mockApiGet({ '/api/v1/admin/accounts': { body: { items: accounts } } });
    const queryClient = new QueryClient();

    expect(await queryClient.fetchQuery(listAccountsQuery)).toEqual(accounts);
    expect(get).toHaveBeenCalledWith('/api/v1/admin/accounts', expect.anything());
  });

  it('signs in as the account with its id in the path', async () => {
    const post = mockApiPost({
      '/api/v1/admin/accounts/{accountId}/impersonate': { body: meImpersonated },
    });
    const queryClient = new QueryClient();

    await new MutationObserver(queryClient, impersonateMutation(queryClient)).mutate(
      meMember.accountId,
    );

    expect(post).toHaveBeenCalledWith('/api/v1/admin/accounts/{accountId}/impersonate', {
      params: { path: { accountId: meMember.accountId } },
    });
  });
});

describe('catalogue administration', () => {
  /** A client with the public and the office's catalogue lists cached. */
  function catalogueClient() {
    const queryClient = new QueryClient();
    queryClient.setQueryData(staffQuery.queryKey, []);
    queryClient.setQueryData(allStaffQuery.queryKey, staffEntries);
    queryClient.setQueryData(courseTypesQuery.queryKey, []);
    queryClient.setQueryData(scheduleQuery(range).queryKey, schedule);
    return queryClient;
  }

  it('lists all staff, inactive ones included', async () => {
    const get = mockApiGet({ '/api/v1/admin/staff': { body: { items: staffEntries } } });

    expect(await new QueryClient().fetchQuery(allStaffQuery)).toEqual(staffEntries);
    expect(get).toHaveBeenCalledWith('/api/v1/admin/staff', expect.anything());
  });

  it('searches the persons by the trimmed words and leaves an empty search out', async () => {
    const get = mockApiGet({ '/api/v1/admin/persons': { body: { items: persons } } });
    const queryClient = new QueryClient();

    expect(await queryClient.fetchQuery(personsQuery('  mia sam '))).toEqual(persons);
    await queryClient.fetchQuery(personsQuery(''));

    expect(get).toHaveBeenCalledWith('/api/v1/admin/persons', {
      params: { query: { q: 'mia sam' } },
      signal: expect.anything(),
    });
    expect(get).toHaveBeenCalledWith('/api/v1/admin/persons', {
      params: { query: {} },
      signal: expect.anything(),
    });
  });

  it('refreshes the public staff list, the office list and the schedule after a staff write', async () => {
    const put = mockApiPut({
      '/api/v1/staff/{staffId}': ({ body }) => ({
        body: { id: ids.staffRiley, ...(body as object) },
      }),
    });
    const queryClient = catalogueClient();
    const body = { displayName: 'Riley Sample', kind: 'instructor' as const, active: false };

    await new MutationObserver(queryClient, updateStaffMutation(queryClient)).mutate({
      staffId: ids.staffRiley,
      body,
    });

    expect(put).toHaveBeenCalledWith('/api/v1/staff/{staffId}', {
      params: { path: { staffId: ids.staffRiley } },
      body,
    });
    for (const key of [
      staffQuery.queryKey,
      allStaffQuery.queryKey,
      scheduleQuery(range).queryKey,
    ]) {
      expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);
    }
  });

  it('refreshes the course types after a course type write', async () => {
    mockApiPost({
      '/api/v1/course-types': { status: 201, body: { id: ids.jazz, name: 'Tap' } },
    });
    const queryClient = catalogueClient();

    await new MutationObserver(queryClient, createCourseTypeMutation(queryClient)).mutate({
      name: 'Tap',
    });

    expect(queryClient.getQueryState(courseTypesQuery.queryKey)?.isInvalidated).toBe(true);
  });

  it('keeps the lists when a staff member is still in use (409)', async () => {
    mockApiDelete({
      '/api/v1/staff/{staffId}': {
        status: 409,
        body: { status: 409, reason: 'in-use', detail: 'staff in use' },
      },
    });
    const queryClient = catalogueClient();

    await expect(
      new MutationObserver(queryClient, deleteStaffMutation(queryClient)).mutate(ids.staffAlex),
    ).rejects.toMatchObject({ status: 409 });
    expect(queryClient.getQueryState(allStaffQuery.queryKey)?.isInvalidated).toBe(false);
  });
});

describe('ApiError', () => {
  it('carries the wait of a 429 from the Retry-After header', async () => {
    mockApiPost({ '/api/v1/auth/password-reset': tooManyRequests(42) });
    const queryClient = new QueryClient();

    const error: unknown = await new MutationObserver(
      queryClient,
      requestPasswordResetMutation(queryClient),
    )
      .mutate({ email: meMember.email })
      .catch((e: unknown) => e);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(429);
    expect((error as ApiError).retryAfter).toBe(42);
  });

  it('has no wait without a Retry-After header', async () => {
    mockApiPost({ '/api/v1/auth/password-reset': { status: 500 } });
    const queryClient = new QueryClient();

    const error: unknown = await new MutationObserver(
      queryClient,
      requestPasswordResetMutation(queryClient),
    )
      .mutate({ email: meMember.email })
      .catch((e: unknown) => e);

    expect((error as ApiError).retryAfter).toBeUndefined();
  });
});

describe('credentials', () => {
  it('requests a reset link and refreshes the demo mailbox', async () => {
    const post = mockApiPost({ '/api/v1/auth/password-reset': { status: 202 } });
    const queryClient = new QueryClient();
    queryClient.setQueryData(['demo', 'mailbox'], []);

    await new MutationObserver(queryClient, requestPasswordResetMutation(queryClient)).mutate({
      email: meMember.email,
      locale: 'en',
    });

    expect(post).toHaveBeenCalledWith('/api/v1/auth/password-reset', {
      body: { email: meMember.email, locale: 'en' },
    });
    expect(queryClient.getQueryState(['demo', 'mailbox'])?.isInvalidated).toBe(true);
  });

  it('resets the password and signs out locally if it was the signed-in account', async () => {
    mockApiGet({ '/api/v1/auth/me': { status: 401 }, '/api/v1/schedule': { body: schedule } });
    const post = mockApiPost({ '/api/v1/auth/password-reset/confirm': { status: 204 } });
    const { queryClient, unsubscribe } = await signedInClient();

    await new MutationObserver(queryClient, resetPasswordMutation(queryClient)).mutate({
      token: 'tok-reset',
      password: 'new-secret-password',
    });

    expect(post).toHaveBeenCalledWith('/api/v1/auth/password-reset/confirm', {
      body: { token: 'tok-reset', password: 'new-secret-password' },
    });
    expect(queryClient.getQueryData(meQuery.queryKey)).toBeNull();
    expect(queryClient.getQueryState(listAccountsQuery.queryKey)).toBeUndefined();
    unsubscribe();
  });

  it('resets the password without asking who is signed in when nobody is', async () => {
    const get = mockApiGet({});
    mockApiPost({ '/api/v1/auth/password-reset/confirm': { status: 204 } });
    const queryClient = new QueryClient();

    await new MutationObserver(queryClient, resetPasswordMutation(queryClient)).mutate({
      token: 'tok-reset',
      password: 'new-secret-password',
    });

    expect(get).not.toHaveBeenCalled();
  });

  it('changes the password and keeps the session', async () => {
    const post = mockApiPost({ '/api/v1/auth/password': { status: 204 } });
    const queryClient = new QueryClient();
    queryClient.setQueryData(meQuery.queryKey, meMember);

    await new MutationObserver(queryClient, changePasswordMutation(queryClient)).mutate({
      currentPassword: 'old-password',
      newPassword: 'new-secret-password',
    });

    expect(post).toHaveBeenCalledWith('/api/v1/auth/password', {
      body: { currentPassword: 'old-password', newPassword: 'new-secret-password' },
    });
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meMember);
  });

  it.each([
    {
      name: 'changePassword',
      mutation: changePasswordMutation,
      path: '/api/v1/auth/password',
      variables: { currentPassword: 'old-password', newPassword: 'new-secret-password' },
    },
    {
      name: 'requestEmailChange',
      mutation: requestEmailChangeMutation,
      path: '/api/v1/auth/email',
      variables: { password: 'secret-password', newEmail: 'nora.new@example.org' },
    },
  ])(
    '$name signs out locally when the session has ended (401)',
    async ({ mutation, path, variables }) => {
      mockApiPost({ [path]: { status: 401 } });
      const queryClient = new QueryClient();
      queryClient.setQueryData(meQuery.queryKey, meMember);

      await expect(
        new MutationObserver(
          queryClient,
          mutation(queryClient) as ReturnType<typeof changePasswordMutation>,
        ).mutate(variables as never),
      ).rejects.toThrow();

      expect(queryClient.getQueryData(meQuery.queryKey)).toBeNull();
    },
  );

  it('requests an email change and refreshes the demo mailbox', async () => {
    const post = mockApiPost({ '/api/v1/auth/email': { status: 202 } });
    const queryClient = new QueryClient();
    queryClient.setQueryData(['demo', 'mailbox'], []);
    queryClient.setQueryData(meQuery.queryKey, meMember);

    await new MutationObserver(queryClient, requestEmailChangeMutation(queryClient)).mutate({
      password: 'secret-password',
      newEmail: 'nora.new@example.org',
      locale: 'de',
    });

    expect(post).toHaveBeenCalledWith('/api/v1/auth/email', {
      body: { password: 'secret-password', newEmail: 'nora.new@example.org', locale: 'de' },
    });
    expect(queryClient.getQueryState(['demo', 'mailbox'])?.isInvalidated).toBe(true);
    // The address changes only once the link is opened.
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meMember);
  });

  it('confirms an email change and refreshes the signed-in account', async () => {
    const moved = { ...meMember, email: 'nora.new@example.org' };
    mockApiGet({ '/api/v1/auth/me': { body: moved } });
    const post = mockApiPost({ '/api/v1/auth/email/confirm': { status: 204 } });
    const queryClient = new QueryClient();
    queryClient.setQueryData(meQuery.queryKey, meMember);

    await new MutationObserver(queryClient, confirmEmailChangeMutation(queryClient)).mutate({
      token: 'tok-email',
      locale: 'en',
    });

    expect(post).toHaveBeenCalledWith('/api/v1/auth/email/confirm', {
      body: { token: 'tok-email', locale: 'en' },
    });
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(moved);
  });
});

describe('account language', () => {
  it.each([
    { name: 'login', mutation: loginMutation, path: '/api/v1/auth/login' },
    { name: 'confirmEmail', mutation: confirmEmailMutation, path: '/api/v1/auth/confirm' },
  ])('$name switches the UI to the account language', async ({ mutation, path }) => {
    mockApiPost({ [path]: { body: { ...meMember, locale: 'de' } } });
    const queryClient = new QueryClient();

    await new MutationObserver(
      queryClient,
      mutation(queryClient) as ReturnType<typeof loginMutation>,
    ).mutate({ email: meMember.email, token: 'tok-123', password: 'secret-password' } as never);

    expect(i18n.resolvedLanguage).toBe('de');
  });

  it('keeps the UI language when the account has none', async () => {
    await i18n.changeLanguage('de');
    mockApiPost({ '/api/v1/auth/login': { body: meMember } });
    const queryClient = new QueryClient();

    await new MutationObserver(queryClient, loginMutation(queryClient)).mutate({
      email: meMember.email,
      password: 'secret-password',
    });

    expect(i18n.resolvedLanguage).toBe('de');
  });

  it('keeps the UI language when an admin signs in as someone else', async () => {
    mockApiPost({
      '/api/v1/admin/accounts/{accountId}/impersonate': {
        body: { ...meImpersonated, locale: 'de' },
      },
    });
    const queryClient = new QueryClient();

    await new MutationObserver(queryClient, impersonateMutation(queryClient)).mutate(
      meMember.accountId,
    );

    expect(i18n.resolvedLanguage).toBe('en');
  });

  it('keeps the UI language when me is fetched again', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: { ...meMember, locale: 'de' } } });
    const queryClient = new QueryClient();

    await queryClient.fetchQuery(meQuery);

    expect(i18n.resolvedLanguage).toBe('en');
  });

  it.each([
    { body: { locale: 'de' as const }, me: { ...meMember, locale: 'de' as const } },
    { body: {}, me: meMember },
  ])('saves the language for emails $body', async ({ body, me }) => {
    const put = mockApiPut({ '/api/v1/auth/locale': { status: 204 } });
    const queryClient = new QueryClient();
    const before: Me = { ...meMember, locale: 'en' };
    queryClient.setQueryData(meQuery.queryKey, before);

    await new MutationObserver(queryClient, updateLocaleMutation(queryClient)).mutate(body);

    expect(put).toHaveBeenCalledWith('/api/v1/auth/locale', { body });
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(me);
    // The UI language is the header switcher's business.
    expect(i18n.resolvedLanguage).toBe('en');
  });
});

describe('family', () => {
  const lina = family.dependents[0]!;
  const person = { givenName: 'Nora', familyName: 'Example', phone: '+49 000 0000111' };

  /** A client with me and the family cached, as /account has them. */
  function familyClient() {
    const queryClient = new QueryClient();
    queryClient.setQueryData(meQuery.queryKey, meMember);
    queryClient.setQueryData(familyQuery.queryKey, family);
    return queryClient;
  }

  it('fetches the family', async () => {
    const get = mockApiGet({ '/api/v1/family': { body: family } });

    expect(await new QueryClient().fetchQuery(familyQuery)).toEqual(family);
    expect(get).toHaveBeenCalledWith('/api/v1/family', expect.anything());
  });

  it('updates my person and refreshes the family and the name in the header', async () => {
    const put = mockApiPut({
      '/api/v1/family/me': { body: { id: meMember.personId, ...person } },
    });
    const queryClient = familyClient();

    await new MutationObserver(queryClient, updateMyPersonMutation(queryClient)).mutate(person);

    expect(put).toHaveBeenCalledWith('/api/v1/family/me', { body: person });
    expect(queryClient.getQueryState(familyQuery.queryKey)?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(meQuery.queryKey)?.isInvalidated).toBe(true);
  });

  it('adds a dependent and refreshes the family', async () => {
    const child = { givenName: 'Ida', familyName: 'Example', birthDate: '2021-01-05' };
    const post = mockApiPost({
      '/api/v1/family/dependents': { status: 201, body: { id: lina.id, ...child } },
    });
    const queryClient = familyClient();

    await new MutationObserver(queryClient, addDependentMutation(queryClient)).mutate(child);

    expect(post).toHaveBeenCalledWith('/api/v1/family/dependents', { body: child });
    expect(queryClient.getQueryState(familyQuery.queryKey)?.isInvalidated).toBe(true);
  });

  it('updates a dependent by its id', async () => {
    const child = { givenName: 'Lina', familyName: 'Example' };
    const put = mockApiPut({
      '/api/v1/family/dependents/{personId}': { body: { id: lina.id, ...child } },
    });
    const queryClient = familyClient();

    await new MutationObserver(queryClient, updateDependentMutation(queryClient)).mutate({
      personId: lina.id,
      body: child,
    });

    expect(put).toHaveBeenCalledWith('/api/v1/family/dependents/{personId}', {
      params: { path: { personId: lina.id } },
      body: child,
    });
    expect(queryClient.getQueryState(familyQuery.queryKey)?.isInvalidated).toBe(true);
  });

  it('removes a dependent by its id', async () => {
    const del = mockApiDelete({ '/api/v1/family/dependents/{personId}': { status: 204 } });
    const queryClient = familyClient();

    await new MutationObserver(queryClient, removeDependentMutation(queryClient)).mutate(lina.id);

    expect(del).toHaveBeenCalledWith('/api/v1/family/dependents/{personId}', {
      params: { path: { personId: lina.id } },
    });
    expect(queryClient.getQueryState(familyQuery.queryKey)?.isInvalidated).toBe(true);
  });

  it('keeps everything when a dependent is still in use (409)', async () => {
    mockApiDelete({
      '/api/v1/family/dependents/{personId}': problem(
        409,
        'the dependent has bookings or class memberships',
      ),
    });
    const queryClient = familyClient();

    const error: unknown = await new MutationObserver(
      queryClient,
      removeDependentMutation(queryClient),
    )
      .mutate(lina.id)
      .catch((e: unknown) => e);

    expect((error as ApiError).status).toBe(409);
    expect(queryClient.getQueryState(familyQuery.queryKey)?.isInvalidated).toBe(false);
    expect(queryClient.getQueryData(meQuery.queryKey)).toEqual(meMember);
  });

  it('asks for the family again when the dependent has left it (404)', async () => {
    mockApiDelete({ '/api/v1/family/dependents/{personId}': problem(404, 'resource not found') });
    const queryClient = familyClient();

    await expect(
      new MutationObserver(queryClient, removeDependentMutation(queryClient)).mutate(lina.id),
    ).rejects.toThrow();

    expect(queryClient.getQueryState(familyQuery.queryKey)?.isInvalidated).toBe(true);
  });

  it('signs out locally when the session has ended (401)', async () => {
    mockApiPut({ '/api/v1/family/me': { status: 401 } });
    const queryClient = familyClient();

    await expect(
      new MutationObserver(queryClient, updateMyPersonMutation(queryClient)).mutate(person),
    ).rejects.toThrow();

    expect(queryClient.getQueryData(meQuery.queryKey)).toBeNull();
  });
});
