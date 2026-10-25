import { onlineManager } from '@tanstack/react-query';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { meMember, mockApiDelete, mockApiGet, mockApiPost } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { type DeviceSubscription, pushDevice, pushOwner, setPushOwner } from './device';
import { PushCard } from './PushCard';

const sub: DeviceSubscription = {
  endpoint: 'https://push.example.net/push/abc',
  keys: { p256dh: 'BPublic', auth: 'secret' },
};

const pushOn = { '/api/v1/my/push': { body: { enabled: true, publicKey: 'BVapidKey' } } };

/** This device: what it has, and whether the browser lets the site notify. */
function device({
  current = null as DeviceSubscription | null,
  permission = 'default' as NotificationPermission,
  supported = true,
  serverKey = null as string | null,
} = {}) {
  let subscribed = current;
  vi.spyOn(pushDevice, 'serverKey').mockImplementation(() =>
    Promise.resolve(subscribed ? serverKey : null),
  );
  Object.defineProperty(pushDevice, 'supported', { value: supported, configurable: true });
  vi.spyOn(pushDevice, 'permission').mockImplementation(() => permission);
  vi.spyOn(pushDevice, 'current').mockImplementation(() => Promise.resolve(subscribed));
  const requestPermission = vi
    .spyOn(pushDevice, 'requestPermission')
    .mockImplementation(() => Promise.resolve(permission === 'denied' ? 'denied' : 'granted'));
  const subscribe = vi.spyOn(pushDevice, 'subscribe').mockImplementation(() => {
    subscribed = sub;
    return Promise.resolve(sub);
  });
  const unsubscribe = vi.spyOn(pushDevice, 'unsubscribe').mockImplementation(() => {
    const was = subscribed;
    subscribed = null;
    return Promise.resolve(was);
  });
  return { requestPermission, subscribe, unsubscribe };
}

const card = async () =>
  within(await screen.findByRole('region', { name: 'Notifications on this device' }));

function renderCard() {
  return renderInRouter(<PushCard accountId={meMember.accountId} />, { at: '/account' });
}

beforeEach(() => setPushOwner(null));
afterEach(() => {
  vi.restoreAllMocks();
  setPushOwner(null);
  onlineManager.setOnline(true);
});

describe('PushCard', () => {
  it('is absent while the installation sends no push', async () => {
    device();
    const get = mockApiGet({ '/api/v1/my/push': { body: { enabled: false } } });
    renderCard();
    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/v1/my/push', expect.anything()));
    expect(screen.queryByRole('region', { name: 'Notifications on this device' })).toBeNull();
  });

  it('turns notifications on: asks the permission, subscribes with the key and saves', async () => {
    const d = device();
    mockApiGet(pushOn);
    const post = mockApiPost({ '/api/v1/my/push/subscriptions': { status: 204 } });
    renderCard();

    const toggle = (await card()).getByRole('switch', { name: 'Notifications on this device' });
    await waitFor(() => expect(toggle).toBeEnabled());
    expect(toggle).not.toBeChecked();

    await userEvent.setup().click(toggle);

    await waitFor(() => expect(toggle).toBeChecked());
    expect(d.requestPermission).toHaveBeenCalled();
    expect(d.subscribe).toHaveBeenCalledWith('BVapidKey');
    expect(post).toHaveBeenCalledWith('/api/v1/my/push/subscriptions', {
      body: { ...sub, deviceLabel: expect.any(String) },
    });
    expect(pushOwner()).toBe(meMember.accountId);
    expect(
      (await card()).getByText(/This device gets a notification for every waiting-list invite/),
    ).toBeVisible();
  });

  it('subscribes anew when the server has another key than the subscription', async () => {
    const old = { ...sub, endpoint: 'https://push.example.net/push/old-key' };
    const d = device({ current: old, permission: 'granted', serverKey: 'BOldVapidKey' });
    mockApiGet(pushOn);
    const post = mockApiPost({ '/api/v1/my/push/subscriptions': { status: 204 } });
    renderCard();

    const toggle = (await card()).getByRole('switch', { name: 'Notifications on this device' });
    await waitFor(() => expect(toggle).toBeEnabled());
    await userEvent.setup().click(toggle);

    await waitFor(() => expect(toggle).toBeChecked());
    expect(d.unsubscribe).toHaveBeenCalled();
    expect(d.subscribe).toHaveBeenCalledWith('BVapidKey');
    expect(post).toHaveBeenCalledWith('/api/v1/my/push/subscriptions', {
      body: { ...sub, deviceLabel: expect.any(String) },
    });
  });

  it('keeps a subscription made with the same key', async () => {
    const d = device({ current: sub, permission: 'granted', serverKey: 'BVapidKey' });
    mockApiGet(pushOn);
    mockApiPost({ '/api/v1/my/push/subscriptions': { status: 204 } });
    renderCard();

    const toggle = (await card()).getByRole('switch', { name: 'Notifications on this device' });
    await waitFor(() => expect(toggle).toBeEnabled());
    await userEvent.setup().click(toggle);

    await waitFor(() => expect(toggle).toBeChecked());
    expect(d.unsubscribe).not.toHaveBeenCalled();
    expect(d.subscribe).not.toHaveBeenCalled();
  });

  it('turns them off: the server forgets the device, then the browser', async () => {
    const d = device({ current: sub, permission: 'granted' });
    setPushOwner(meMember.accountId);
    mockApiGet(pushOn);
    const del = mockApiDelete({ '/api/v1/my/push/subscriptions': { status: 204 } });
    renderCard();

    const toggle = (await card()).getByRole('switch', { name: 'Notifications on this device' });
    await waitFor(() => expect(toggle).toBeChecked());
    await userEvent.setup().click(toggle);

    await waitFor(() => expect(toggle).not.toBeChecked());
    expect(del).toHaveBeenCalledWith('/api/v1/my/push/subscriptions', {
      body: { endpoint: sub.endpoint },
    });
    expect(d.unsubscribe).toHaveBeenCalled();
    expect(pushOwner()).toBeNull();
  });

  it("shows another account's subscription on this device as off", async () => {
    device({ current: sub, permission: 'granted' });
    setPushOwner('someone-else');
    mockApiGet(pushOn);
    renderCard();

    const toggle = (await card()).getByRole('switch', { name: 'Notifications on this device' });
    await waitFor(() => expect(toggle).toBeEnabled());
    expect(toggle).not.toBeChecked();
  });

  it('says so when the browser blocks notifications', async () => {
    device({ permission: 'denied' });
    mockApiGet(pushOn);
    renderCard();

    const c = await card();
    expect(await c.findByText(/Your browser blocks notifications from this site/)).toBeVisible();
    expect(c.getByRole('switch', { name: 'Notifications on this device' })).toBeDisabled();
  });

  it('says so when the browser cannot show notifications', async () => {
    device({ supported: false });
    mockApiGet(pushOn);
    renderCard();

    expect(await (await card()).findByText(/This browser cannot show notifications/)).toBeVisible();
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it('cannot be switched offline', async () => {
    device();
    mockApiGet(pushOn);
    renderCard();
    const toggle = (await card()).getByRole('switch', { name: 'Notifications on this device' });
    await waitFor(() => expect(toggle).toBeEnabled());

    onlineManager.setOnline(false);
    await waitFor(() => expect(toggle).toBeDisabled());
    expect(toggle).toHaveAttribute('title', 'Not available offline');
  });
});
