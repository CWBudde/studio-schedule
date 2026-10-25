import { dehydrate, onlineManager, QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Persister } from '@tanstack/react-query-persist-client';
import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  logoutMutation,
  meQuery,
  myLessonsQuery,
  myPassesQuery,
  publicSettingsQuery,
  scheduleQuery,
  sessionCaches,
} from '@/api/queries';
import { transportFetch } from '@/api/transport';
import { OfflineBanner } from '@/components/layout/OfflineBanner';
import { Button } from '@/components/ui/button';
import { pushDevice, pushOwner, setPushOwner } from '@/features/push/device';
import { meMember, mockApiDelete, mockApiGet, mockApiPost, publicSettings } from '@/test/mockApi';
import { isPersistedKey, offlineCacheOptions, setOfflinePersister } from './offlineCache';
import { OfflineError } from './online';

afterEach(() => {
  vi.restoreAllMocks();
  onlineManager.setOnline(true);
  setOfflinePersister(null);
  setPushOwner(null);
});

function fakePersister(): Persister & { removeClient: ReturnType<typeof vi.fn> } {
  return {
    persistClient: vi.fn(),
    restoreClient: vi.fn(() => undefined),
    removeClient: vi.fn(() => undefined),
  };
}

describe('the offline cache', () => {
  it('keeps only the allow-listed queries', () => {
    for (const key of [
      ['settings', 'public'],
      ['schedule', { from: '2026-10-26', to: '2026-11-09' }],
      ['lesson', 'l1'],
      ['my-lessons', 'upcoming'],
      ['auth', 'me'],
    ]) {
      expect(isPersistedKey(key), JSON.stringify(key)).toBe(true);
    }
    for (const key of [
      ['settings', 'logo'],
      ['my-lessons', 'history'],
      ['family'],
      ['my-passes'],
      ['admin', 'accounts'],
      ['calendar-feeds'],
      ['push', 'status'],
    ]) {
      expect(isPersistedKey(key), JSON.stringify(key)).toBe(false);
    }
  });

  it('saves the allow-listed queries with data, and nothing else', () => {
    const client = new QueryClient();
    client.setQueryData(meQuery.queryKey, meMember);
    client.setQueryData(publicSettingsQuery.queryKey, publicSettings as never);
    client.setQueryData(myLessonsQuery.queryKey, { upcoming: [], waitlist: [] } as never);
    client.setQueryData(myPassesQuery.queryKey, { items: [] } as never);
    client.setQueryData(scheduleQuery({ from: '2026-10-26', to: '2026-11-09' }).queryKey, {
      lessons: [],
    } as never);

    const options = offlineCacheOptions(fakePersister(), 'v1');
    const saved = dehydrate(client, options.dehydrateOptions).queries.map((q) => q.queryKey[0]);

    expect(saved.sort()).toEqual(['auth', 'my-lessons', 'schedule', 'settings']);
    expect(options.maxAge).toBe(7 * 24 * 60 * 60 * 1000);
    expect(options.buster).toBe('v1');
  });

  /** A client with the app's caches (router.ts) and a signed-in member. */
  function appClient(): QueryClient {
    const client: QueryClient = new QueryClient({
      ...sessionCaches(() => client),
      defaultOptions: { queries: { retry: false } },
    });
    client.setQueryData(meQuery.queryKey, meMember);
    return client;
  }

  it('is deleted on a 401', async () => {
    const persister = fakePersister();
    setOfflinePersister(persister);
    mockApiGet({ '/api/v1/my/passes': { status: 401 } });
    const client = appClient();

    await expect(client.fetchQuery(myPassesQuery)).rejects.toThrow();

    expect(persister.removeClient).toHaveBeenCalled();
  });

  it('is deleted on sign-out, and the device stops getting notifications', async () => {
    const persister = fakePersister();
    setOfflinePersister(persister);
    setPushOwner(meMember.accountId);
    const sub = { endpoint: 'https://push.example.net/x', keys: { p256dh: 'p', auth: 'a' } };
    vi.spyOn(pushDevice, 'current').mockResolvedValue(sub);
    const unsubscribe = vi.spyOn(pushDevice, 'unsubscribe').mockResolvedValue(sub);
    const del = mockApiDelete({ '/api/v1/my/push/subscriptions': { status: 204 } });
    mockApiPost({ '/api/v1/auth/logout': { status: 204 } });
    const client = appClient();

    await client.getMutationCache().build(client, logoutMutation(client)).execute(undefined);

    expect(del).toHaveBeenCalledWith('/api/v1/my/push/subscriptions', {
      body: { endpoint: sub.endpoint },
    });
    expect(unsubscribe).toHaveBeenCalled();
    expect(pushOwner()).toBeNull();
    expect(persister.removeClient).toHaveBeenCalled();
    expect(client.getQueryData(meQuery.queryKey)).toBeNull();
  });
});

describe('offline', () => {
  function renderWithClient(ui: React.ReactNode) {
    return render(<QueryClientProvider client={new QueryClient()}>{ui}</QueryClientProvider>);
  }

  it('shows the banner while offline', async () => {
    renderWithClient(<OfflineBanner />);
    expect(screen.queryByText(/You are offline/)).toBeNull();

    onlineManager.setOnline(false);
    expect(await screen.findByText(/You are offline/)).toBeVisible();

    onlineManager.setOnline(true);
    await waitFor(() => expect(screen.queryByText(/You are offline/)).toBeNull());
  });

  it('disables the buttons that change data, with a tooltip', async () => {
    renderWithClient(
      <form>
        <Button type="submit">Save</Button>
        <Button requiresOnline>Book</Button>
        <Button>Next week</Button>
      </form>,
    );
    onlineManager.setOnline(false);

    for (const name of ['Save', 'Book']) {
      const button = await screen.findByRole('button', { name });
      await waitFor(() => expect(button).toBeDisabled());
      expect(button).toHaveAttribute('title', 'Not available offline');
    }
    expect(screen.getByRole('button', { name: 'Next week' })).toBeEnabled();

    onlineManager.setOnline(true);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled());
  });

  it('sends no write: the transport refuses it', async () => {
    const fetch = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}'));
    onlineManager.setOnline(false);

    await expect(
      transportFetch(new Request('http://localhost/api/v1/lessons/x/bookings', { method: 'POST' })),
    ).rejects.toBeInstanceOf(OfflineError);
    await expect(
      transportFetch('/api/v1/my/push/subscriptions', { method: 'DELETE' }),
    ).rejects.toBeInstanceOf(OfflineError);
    expect(fetch).not.toHaveBeenCalled();

    // Reads go out (the queries pause by themselves).
    await transportFetch('/api/v1/schedule');
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
