import { MutationObserver, QueryClient, QueryObserver } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  accounts,
  meAdmin,
  meImpersonated,
  meMember,
  mockApiGet,
  mockApiPost,
  schedule,
  tooManyRequests,
} from '@/test/mockApi';
import {
  ApiError,
  changePasswordMutation,
  confirmEmailChangeMutation,
  confirmEmailMutation,
  impersonateMutation,
  listAccountsQuery,
  loginMutation,
  logoutMutation,
  meQuery,
  requestEmailChangeMutation,
  requestPasswordResetMutation,
  resetPasswordMutation,
  scheduleQuery,
  stopImpersonationMutation,
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
