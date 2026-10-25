import { test as base } from '@playwright/test';
import { expect, gotoDemo, type Page, signIn, test } from './fixtures';

// The app as a PWA (ADR 0017): installable with the demo's manifest, a
// service worker that starts it offline, the last schedule and My lessons
// read-only offline, and the demo's web push shown as a real notification.

/** Waits until the service worker is active and controls nothing yet but has precached the shell. */
async function serviceWorkerReady(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const reg = await navigator.serviceWorker.ready;
    return reg.scope;
  });
}

test('the demo links a valid manifest and registers its service worker', async ({
  page,
  request,
}) => {
  await gotoDemo(page, '/');

  const href = await page.locator('link[rel="manifest"]').getAttribute('href');
  expect(href).toBe('/studio-schedule/manifest.webmanifest');
  const res = await request.get(href ?? '');
  expect(res.ok()).toBe(true);
  const manifest = (await res.json()) as {
    name: string;
    start_url: string;
    scope: string;
    display: string;
    icons: { src: string; sizes: string; purpose?: string }[];
  };
  expect(manifest.name).toBe('Studio Schedule (demo)');
  expect(manifest).toMatchObject({
    start_url: '/studio-schedule/',
    scope: '/studio-schedule/',
    display: 'standalone',
  });
  expect(manifest.icons.map((i) => i.sizes)).toEqual(['192x192', '512x512', '512x512']);
  expect(manifest.icons.some((i) => i.purpose === 'maskable')).toBe(true);
  for (const icon of manifest.icons) {
    const png = await request.get(`/studio-schedule/${icon.src}`);
    expect(png.headers()['content-type']).toBe('image/png');
  }

  expect(await serviceWorkerReady(page)).toMatch(/\/studio-schedule\/$/);
});

/**
 * Offline the demo's backend cannot load (its WebAssembly is not cached),
 * and the browser logs the failed requests: only those may show.
 */
const offlineTest = base.extend<{ pageErrors: string[] }>({
  pageErrors: async ({ page }, use) => {
    const errors: string[] = [];
    page.on('console', (msg) => {
      if (
        msg.type() === 'error' &&
        !/ERR_INTERNET_DISCONNECTED|Failed to (load|fetch)/.test(msg.text())
      )
        errors.push(`console: ${msg.text()}`);
    });
    page.on('pageerror', (err) => {
      if (!/Failed to (load|fetch)|demo backend/i.test(err.message))
        errors.push(`page: ${err.message}`);
    });
    await use(errors);
  },
});

offlineTest(
  'offline, the last schedule and My lessons show read-only after a reload',
  async ({ page, context, pageErrors }) => {
    const name = await signIn(page, 'Member');
    const nav = page.getByRole('navigation', { name: 'Main' });

    // Online: My lessons, then the schedule with a lesson's sheet, so all
    // of them are in the offline cache.
    await nav.getByRole('link', { name: 'My lessons' }).click();
    const upcoming = page.getByRole('region', { name: 'Coming up' });
    await expect(upcoming.getByRole('listitem').first()).toBeVisible();
    const cancelButtons = upcoming.getByRole('button', { name: /^Cancel the booking of / });
    await expect(cancelButtons.first()).toBeEnabled();

    await nav.getByRole('link', { name: 'Schedule' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
    const card = page
      .getByRole('main')
      .getByRole('button', { name: /\d{2}:\d{2}–\d{2}:\d{2}/ })
      .filter({ hasText: /\d+ free/ })
      .filter({ hasNotText: 'Booked' })
      .first();
    const cardName = (await card.getAttribute('aria-label')) ?? '';
    await card.click();
    const sheet = page.getByRole('dialog', { name: cardName.split(', ')[0] ?? '' });
    const book = sheet
      .getByRole('region', { name: 'Booking' })
      .getByRole('button', { name: 'Book', exact: true });
    await expect(book).toBeEnabled();
    await sheet.getByRole('button', { name: 'Close' }).click();
    await expect(sheet).toBeHidden();

    await serviceWorkerReady(page);
    // The cache is saved a moment after the last change.
    await page.waitForTimeout(1_500);

    await context.setOffline(true);
    await page.reload();

    await expect(page.getByText('You are offline.')).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
    await expect(page.getByRole('main').getByRole('button', { name: cardName })).toBeVisible();
    await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();

    // Booking is off: the button says why.
    await page.getByRole('main').getByRole('button', { name: cardName }).click();
    await expect(book).toBeDisabled();
    await expect(book).toHaveAttribute('title', 'Not available offline');
    await sheet.getByRole('button', { name: 'Close' }).click();

    await nav.getByRole('link', { name: 'My lessons' }).click();
    await expect(upcoming.getByRole('listitem').first()).toBeVisible();
    await expect(cancelButtons.first()).toBeDisabled();

    await context.setOffline(false);
    await expect(page.getByText('You are offline.')).toBeHidden();
    expect(pageErrors).toEqual([]);
  },
);
