import { expect, gotoDemo, type Page, signIn, signOut, test } from './fixtures';

// The Werkstatt demo school (docs/demo-mode.md, "Two demo schools") in its
// own theme (ADR 0018). Its language is German; the visitor here chose
// English, so the shared helpers' names apply.

test.use({ demoSchool: 'werkstatt' });

test.beforeEach(async ({ context }) => {
  await context.addInitScript(() => localStorage.setItem('studio-schedule.lang', 'en'));
});

/** The computed style of the page's body. */
const bodyStyle = (page: Page) =>
  page.evaluate(() => {
    const s = getComputedStyle(document.body);
    return {
      fontFamily: s.fontFamily,
      backgroundColor: s.backgroundColor,
      backgroundImage: s.backgroundImage,
    };
  });

test("shows the Werkstatt look: its font and the school's background", async ({ page }) => {
  await gotoDemo(page, '/');

  await expect(page.locator('html')).toHaveAttribute('data-theme', 'werkstatt');
  expect((await bodyStyle(page)).fontFamily).toMatch(/^"?Roboto Condensed"?/);
  // The seeded background under a light wash, once the settings and the image have arrived.
  await expect
    .poll(async () => (await bodyStyle(page)).backgroundImage)
    .toMatch(/^linear-gradient\(.+\), url\("data:image\/jpeg;base64,/);
  await expect
    .poll(() => page.evaluate(() => document.fonts.check('16px "Roboto Condensed"')))
    .toBe(true);
});

test('a school without a background image gets a plain warm-white page', async ({ page }) => {
  const name = await signIn(page, 'Administrator');
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Settings' })
    .click();
  const card = page.getByRole('region', { name: 'Branding' });
  await card.getByRole('button', { name: 'Remove the background image' }).click();
  await expect(page.getByText('The background image has been removed.')).toBeVisible();

  // No image and no wash, not a broken image or a dark gap (werkstatt.css, body).
  await expect.poll(async () => (await bodyStyle(page)).backgroundImage).toBe('none');
  expect((await bodyStyle(page)).backgroundColor).toBe('rgb(251, 250, 248)');
});

test('signs in and out with the header button', async ({ page }) => {
  await gotoDemo(page, '/');
  const banner = page.getByRole('banner');
  await expect(banner.getByRole('link', { name: 'Sign in' })).toBeVisible();

  const name = await signIn(page, 'Member');

  await expect(banner.getByRole('link', { name: 'Sign in' })).toHaveCount(0);
  await banner.getByRole('button', { name: 'Sign out' }).click();
  await expect(page.getByText('You have been signed out.')).toBeVisible();
  await expect(banner.getByRole('link', { name: 'Sign in' })).toBeVisible();
  await expect(banner.getByRole('button', { name })).toHaveCount(0);
});

test('the account menu still signs out too', async ({ page }) => {
  const name = await signIn(page, 'Member');

  await signOut(page, name);

  await expect(page.getByRole('banner').getByRole('link', { name: 'Sign in' })).toBeVisible();
});

test('dialogs get the warm scrim', async ({ page }) => {
  await gotoDemo(page, '/');

  await page.getByRole('banner').getByRole('button', { name: 'Reset demo' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Reset the demo?' });
  await expect(dialog).toBeVisible();
  const overlay = page.locator('[data-slot="alert-dialog-overlay"]');
  await expect(overlay).toHaveCSS('background-color', 'rgba(50, 32, 14, 0.4)');
  await expect(overlay).toHaveCSS('backdrop-filter', 'blur(2px)');

  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
});

test.describe('the navigation', () => {
  test('desktop: tabs below the header, the current one marked', async ({ page, isMobile }) => {
    test.skip(isMobile, 'phones get the menu');
    await gotoDemo(page, '/');

    const nav = page.getByRole('navigation', { name: 'Main' });
    await expect(page.getByRole('button', { name: 'Menu' })).toBeHidden();
    await nav.getByRole('link', { name: 'Classes' }).click();

    await expect(page.getByRole('heading', { level: 1, name: 'Classes' })).toBeVisible();
    await expect(nav.getByRole('link', { name: 'Classes' })).toHaveAttribute(
      'data-status',
      'active',
    );
  });

  test('phone: the menu opens, navigates and closes', async ({ page, isMobile }) => {
    test.skip(!isMobile, 'wider screens get tabs');
    await gotoDemo(page, '/');

    const menu = page.getByRole('button', { name: 'Menu' });
    await expect(page.getByRole('navigation', { name: 'Main' })).toHaveCount(0);
    await menu.click();
    await expect(menu).toHaveAttribute('aria-expanded', 'true');
    const nav = page.getByRole('navigation', { name: 'Main' });
    await expect(nav.getByRole('link', { name: 'Schedule' })).toBeFocused();

    await page.keyboard.press('Escape');
    await expect(nav).toHaveCount(0);
    await expect(menu).toBeFocused();

    await menu.click();
    await nav.getByRole('link', { name: 'Classes' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Classes' })).toBeVisible();
    await expect(nav).toHaveCount(0);
    await expect(menu).toHaveAttribute('aria-expanded', 'false');
  });
});
