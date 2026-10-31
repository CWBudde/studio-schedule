import { expect, gotoDemo, lessons, type Page, signIn, signOut, test } from './fixtures';

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

test.describe('the schedule', () => {
  /** The school's rooms (internal/seed/demo.go, werkstattLocations). */
  const rooms = /^(Oben|Unten|Trainingsraum)$/;

  test('desktop: each day in room columns, none of them empty', async ({ page, isMobile }) => {
    test.skip(isMobile, 'phones get one column');
    await gotoDemo(page, '/schedule');
    const day = page.getByRole('main').getByRole('region').first();
    await expect(lessons(day).first()).toBeVisible();

    const columns = day.getByRole('list', { name: rooms });
    const count = await columns.count();
    expect(count).toBeGreaterThan(0);
    // Exactly the rooms the day uses: no other list, and each column holds lessons.
    await expect(day.getByRole('list')).toHaveCount(count);
    for (let i = 0; i < count; i++) {
      await expect(columns.nth(i).getByRole('listitem').first()).toBeVisible();
    }
    // Side by side, not squeezed: each column at least 13rem wide.
    for (let i = 0; i < count; i++) {
      const box = await columns.nth(i).boundingBox();
      expect(box?.width ?? 0).toBeGreaterThanOrEqual(13 * 16 - 1);
    }
  });

  test('phone: one column of cards, no room columns', async ({ page, isMobile }) => {
    test.skip(!isMobile, 'wider screens get room columns');
    await gotoDemo(page, '/schedule');
    const day = page.getByRole('main').getByRole('region').first();
    await expect(lessons(day).first()).toBeVisible();

    await expect(day.getByRole('list')).toHaveCount(1);
    await expect(day.getByRole('list', { name: rooms })).toHaveCount(0);
  });

  test("a guardian's square button opens the lesson, never books a guess", async ({ page }) => {
    // The demo member books for their children too (review focus 3): the
    // card cannot know whom to book, so its button opens the sheet.
    await signIn(page, 'Member');
    await gotoDemo(page, '/schedule');
    const main = page.getByRole('main');
    const details = main.getByRole('button', { name: 'Details', exact: true });
    await expect(details.first()).toBeVisible();
    await expect(main.getByRole('button', { name: 'Book', exact: true })).toHaveCount(0);

    // `has` is relative to each group, so not the `main`-scoped locator.
    const card = main
      .getByRole('group')
      .filter({ has: page.getByRole('button', { name: 'Details', exact: true }) })
      .first();
    const label = (await card.getAttribute('aria-label')) ?? '';
    const course = label.slice(0, label.indexOf(','));
    await card.getByRole('button', { name: 'Details', exact: true }).click();
    await expect(page.getByRole('dialog').getByRole('heading', { name: course })).toBeVisible();
  });
});
