import { expect, gotoDemo, type Page, test } from './fixtures';

// A first visit: the demo opens with its about page.
test.use({ demoEntered: false });

/** Collects the requests for the Go backend's WebAssembly. */
function watchBackend(page: Page): string[] {
  const requests: string[] = [];
  page.on('request', (request) => {
    if (request.url().endsWith('/app.wasm')) requests.push(request.url());
  });
  return requests;
}

test('a first visit shows the about page and starts no backend', async ({ page }) => {
  const backend = watchBackend(page);
  await page.goto('');

  await expect(page.getByRole('heading', { level: 1, name: 'Studio Schedule' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'How the demo works' })).toBeVisible();
  expect(new URL(page.url()).hash).toBe('#/about');
  const password = (await page.getByRole('main').locator('code').textContent()) ?? '';
  await expect(page.getByRole('complementary', { name: 'Demo mode' })).toHaveCount(0);
  expect(backend).toEqual([]);

  // The password it names is the one of the demo accounts.
  await page.getByRole('link', { name: 'Sign in with a demo account' }).click();
  await expect(
    page.getByRole('region', { name: 'Demo accounts' }).getByText(password).first(),
  ).toBeVisible({ timeout: 30_000 });
});

test('opening the demo starts it, and the next visit goes straight in', async ({ page }) => {
  const backend = watchBackend(page);
  await page.goto('');

  await page.getByRole('link', { name: 'Open the demo' }).first().click();

  const status = page.getByRole('region', { name: 'System status' });
  await expect(status.getByText('Backend: OK')).toBeVisible({ timeout: 30_000 });
  expect(new URL(page.url()).hash).toBe('#/');
  expect(backend).toHaveLength(1);

  await page.reload();
  await expect(status.getByText('Backend: OK')).toBeVisible({ timeout: 30_000 });
  expect(new URL(page.url()).hash).toBe('#/');
});

test('a deep link skips the about page, and the banner leads to it', async ({ page }) => {
  await gotoDemo(page, '/schedule');
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();

  await page
    .getByRole('complementary', { name: 'Demo mode' })
    .getByRole('link', { name: 'About this demo' })
    .click();

  await expect(page.getByRole('heading', { name: 'How the demo works' })).toBeVisible();
  await page.getByRole('link', { name: 'Open the demo' }).first().click();
  await expect(page.getByRole('region', { name: 'Next lessons' })).toBeVisible();
});

test('the about page speaks German', async ({ page }) => {
  await page.goto('');
  await page.getByRole('button', { name: 'Change language' }).click();
  await page.getByRole('menuitemradio', { name: 'Deutsch' }).click();

  await expect(page.getByRole('heading', { name: 'So funktioniert die Demo' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Demo öffnen' }).first()).toBeVisible();
});
