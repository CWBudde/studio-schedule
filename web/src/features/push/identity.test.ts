import { QueryClient } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Me } from '@/api/client';
import {
  changePasswordMutation,
  loginMutation,
  logoutMutation,
  meQuery,
  myPassesQuery,
  sessionCaches,
} from '@/api/queries';
import {
  meAdmin,
  meImpersonated,
  meMember,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
} from '@/test/mockApi';
import { type DeviceSubscription, pushDevice, pushOwner, setPushOwner } from './device';

// Notifications on a device belong to the account that turned them on
// (ADR 0017): they end when that account leaves the device, never for
// another account's sign-out.

const sub: DeviceSubscription = {
  endpoint: 'https://push.example.net/push/device',
  keys: { p256dh: 'p', auth: 'a' },
};

let unsubscribe: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.spyOn(pushDevice, 'current').mockResolvedValue(sub);
  unsubscribe = vi.spyOn(pushDevice, 'unsubscribe').mockResolvedValue(sub) as never;
});

afterEach(() => {
  vi.restoreAllMocks();
  setPushOwner(null);
});

/** A client with the app's caches (router.ts), signed in as `me`. */
function appClient(me: Me): QueryClient {
  const client: QueryClient = new QueryClient({
    ...sessionCaches(() => client),
    defaultOptions: { queries: { retry: false } },
  });
  client.setQueryData(meQuery.queryKey, me);
  return client;
}

const run = (client: QueryClient, options: unknown, vars: unknown) =>
  client
    .getMutationCache()
    .build(client, options as never)
    .execute(vars as never);

describe('notifications on this device', () => {
  it('end on a 401 of their account: the browser unsubscribes', async () => {
    setPushOwner(meMember.accountId);
    const del = mockApiDelete({});
    mockApiGet({ '/api/v1/my/passes': { status: 401 } });
    const client = appClient(meMember);

    await expect(client.fetchQuery(myPassesQuery)).rejects.toThrow();

    await vi.waitFor(() => expect(pushOwner()).toBeNull());
    expect(unsubscribe).toHaveBeenCalled();
    // Not signed in any more: there is nobody to delete the row as.
    expect(del).not.toHaveBeenCalled();
  });

  it('end when another account signs in on the device', async () => {
    setPushOwner(meMember.accountId);
    const del = mockApiDelete({ '/api/v1/my/push/subscriptions': { status: 204 } });
    mockApiPost({ '/api/v1/auth/login': { body: meOffice } });
    const client = appClient(meMember);

    await run(client, loginMutation(client), { email: meOffice.email, password: 'x' });

    await vi.waitFor(() => expect(pushOwner()).toBeNull());
    expect(del).toHaveBeenCalledWith('/api/v1/my/push/subscriptions', {
      body: { endpoint: sub.endpoint },
    });
    expect(unsubscribe).toHaveBeenCalled();
  });

  it("stay the admin's while the admin signs in as someone", async () => {
    setPushOwner(meAdmin.accountId);
    mockApiPost({ '/api/v1/auth/login': { body: meImpersonated } });
    const client = appClient(meAdmin);

    await run(client, loginMutation(client), { email: meMember.email, password: 'x' });

    await new Promise((r) => setTimeout(r, 0));
    expect(pushOwner()).toBe(meAdmin.accountId);
    expect(unsubscribe).not.toHaveBeenCalled();
  });

  it("stay another account's when someone else signs out", async () => {
    setPushOwner(meAdmin.accountId);
    const del = mockApiDelete({ '/api/v1/my/push/subscriptions': { status: 204 } });
    mockApiPost({ '/api/v1/auth/logout': { status: 204 } });
    const client = appClient(meOffice);

    await run(client, logoutMutation(client), undefined);

    await new Promise((r) => setTimeout(r, 0));
    expect(del).not.toHaveBeenCalled();
    expect(unsubscribe).not.toHaveBeenCalled();
    expect(pushOwner()).toBe(meAdmin.accountId);
  });

  it("stay the account's while the server could not forget the device", async () => {
    setPushOwner(meMember.accountId);
    mockApiDelete({ '/api/v1/my/push/subscriptions': { status: 500 } });
    // The sign-out fails too: still signed in, still the account's device.
    mockApiPost({ '/api/v1/auth/logout': { status: 500 } });
    const client = appClient(meMember);

    await expect(run(client, logoutMutation(client), undefined)).rejects.toThrow();

    expect(unsubscribe).not.toHaveBeenCalled();
    expect(pushOwner()).toBe(meMember.accountId);
  });

  it('are saved again after a password change, which ended them on the server', async () => {
    setPushOwner(meMember.accountId);
    const post = mockApiPost({
      '/api/v1/auth/password': { status: 204 },
      '/api/v1/my/push/subscriptions': { status: 204 },
    });
    const client = appClient(meMember);

    await run(client, changePasswordMutation as never, {
      currentPassword: 'old password',
      newPassword: 'a new password',
    });

    await vi.waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/v1/my/push/subscriptions', {
        body: { ...sub, deviceLabel: expect.any(String) },
      }),
    );
    expect(pushOwner()).toBe(meMember.accountId);
  });
});
