import { expect, gotoDemo, type Page, signIn, test } from './fixtures';

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

// The two demo schools (docs/demo-mode.md, "Two demo schools"). The
// Werkstatt school's language is German: without a visitor's own choice the
// UI follows it, so its names may be either.

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** The header's home link of a school, in either language. */
function schoolHome(page: Page, school: string) {
  return page
    .getByRole('banner')
    .getByRole('link', { name: new RegExp(`^${escape(school)} – (home|Startseite)$`) });
}

const programme = 'Studio Demo — Ballet & Dance';
const werkstatt = 'Tanzhaus am Lindenhof';

/** The banner's button that switches to the other school's look. */
function switchTo(page: Page, look: 'Werkstatt' | 'Programme') {
  return page.getByRole('complementary', { name: /^(Demo mode|Demo-Modus)$/ }).getByRole('button', {
    name: new RegExp(`^(Switch to the ${look} look|Zur ${look}-Optik wechseln)$`),
  });
}

test('"Open in the Werkstatt look" opens the Werkstatt school', async ({ page }) => {
  await page.goto('');

  await page.getByRole('link', { name: 'Open in the Werkstatt look' }).click();

  await expect(schoolHome(page, werkstatt)).toBeVisible({ timeout: 30_000 });
  await expect(page.locator('html')).toHaveAttribute('lang', 'de');
  expect(new URL(page.url()).hash).toBe('#/');
  await expect(switchTo(page, 'Programme')).toBeVisible();
});

test.describe('in the Werkstatt school', () => {
  test.use({ demoEntered: true, demoSchool: 'werkstatt' });

  test('the banner switches back to the programme school, and it stays', async ({ page }) => {
    await gotoDemo(page, '/');
    await expect(schoolHome(page, werkstatt)).toBeVisible();

    await switchTo(page, 'Programme').click();

    await expect(schoolHome(page, programme)).toBeVisible({ timeout: 30_000 });
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(switchTo(page, 'Werkstatt')).toBeVisible();
    expect(new URL(page.url()).hash).toBe('#/');

    // A reload keeps the visitor's choice.
    await page.reload();
    await expect(schoolHome(page, programme)).toBeVisible({ timeout: 30_000 });
  });
});

test.describe('switching schools', () => {
  test.use({ demoEntered: true });

  test('each school keeps its data: a booking survives a visit to the other', async ({ page }) => {
    const name = await signIn(page, 'Member');
    const nav = page.getByRole('navigation', { name: 'Main' });

    // Book a workshop, as in my-lessons.spec.ts.
    await nav.getByRole('link', { name: 'Schedule' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
    await page.getByRole('button', { name: 'Next 14 days' }).click();
    const card = page
      .getByRole('main')
      .getByRole('button', { name: /\d{2}:\d{2}–\d{2}:\d{2}, Workshop$/ })
      .filter({ hasNotText: 'Booked' })
      .filter({ hasText: /\d+ free/ })
      .first();
    await expect(card).toBeVisible();
    const cardName = (await card.getAttribute('aria-label')) ?? '';
    const courseType = cardName.split(', ')[0] ?? '';
    await card.click();
    const sheet = page.getByRole('dialog', { name: courseType });
    await sheet
      .getByRole('region', { name: 'Booking' })
      .getByRole('button', { name: 'Book', exact: true })
      .click();
    await expect(page.getByText(`${name} is booked.`)).toBeVisible();
    await sheet.getByRole('button', { name: 'Close' }).click();
    await expect(sheet).toBeHidden();

    // The switch saves the booking before it reloads (transport.ts, switchSchool).
    // The Werkstatt school has data of its own, and nobody is signed in there.
    await switchTo(page, 'Werkstatt').click();
    await expect(schoolHome(page, werkstatt)).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole('banner').getByRole('button', { name })).toHaveCount(0);

    // Back in the programme school, the member is signed in and booked.
    await switchTo(page, 'Programme').click();
    await expect(schoolHome(page, programme)).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
    await nav.getByRole('link', { name: 'My lessons' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'My lessons' })).toBeVisible();
    const lesson = new RegExp(`^${escape(cardName.replace(/, Workshop$/, ''))}(, .+)?$`);
    const row = page
      .getByRole('region', { name: 'Coming up' })
      .getByRole('listitem')
      .filter({ has: page.getByRole('button', { name: lesson }) });
    await expect(row.getByText('Booked', { exact: true })).toBeVisible();
  });
});
