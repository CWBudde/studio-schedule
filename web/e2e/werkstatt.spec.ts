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

  test('desktop: the columns follow the order of the rooms, not their names', async ({
    page,
    isMobile,
  }) => {
    test.skip(isMobile, 'phones get one column');
    await gotoDemo(page, '/schedule');
    const days = page.getByRole('main').getByRole('region');
    await expect(lessons(days.first()).first()).toBeVisible();

    // The rooms' positions (internal/seed/demo.go); a day shows the rooms it uses.
    const order = ['Oben', 'Unten', 'Trainingsraum'];
    const count = await days.count();
    expect(count).toBeGreaterThan(0);
    for (let i = 0; i < count; i++) {
      const names = await days
        .nth(i)
        .getByRole('list', { name: rooms })
        .evaluateAll((lists) =>
          lists.map(
            (list) =>
              document.getElementById(list.getAttribute('aria-labelledby') ?? '')?.textContent ??
              '',
          ),
        );
      const places = names.map((name) => order.indexOf(name));
      expect(places).not.toContain(-1);
      expect(places).toEqual([...places].sort((a, b) => a - b));
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

  test('days on bands in their tone, rooms named above their columns', async ({
    page,
    isMobile,
  }) => {
    await gotoDemo(page, '/schedule');
    const days = page.getByRole('main').getByRole('region');
    await expect(lessons(days.first()).first()).toBeVisible();

    // The light tones (werkstatt.css): today, tomorrow, any later day. The
    // list starts today, so no day is past; which tones show depends on the
    // weekday (a day without lessons has no band).
    const tones = ['rgb(216, 27, 111)', 'rgb(251, 236, 25)', 'rgb(104, 179, 201)'];
    const count = await days.count();
    expect(count).toBeGreaterThan(0);
    for (let i = 0; i < count; i++) {
      const heading = days.nth(i).getByRole('heading', { level: 2 });
      const band = await heading.evaluate((el) => ({
        background: getComputedStyle(el.parentElement ?? el).backgroundColor,
        transform: getComputedStyle(el).textTransform,
      }));
      expect(tones).toContain(band.background);
      expect(band.transform).toBe('uppercase');
    }

    // Desktop: the grey row names each column's room (the list's name).
    if (isMobile) return;
    const columns = days.first().getByRole('list', { name: rooms });
    const columnCount = await columns.count();
    expect(columnCount).toBeGreaterThan(0);
    for (let i = 0; i < columnCount; i++) {
      const id = (await columns.nth(i).getAttribute('aria-labelledby')) ?? '';
      const header = page.locator(`[id="${id}"]`);
      await expect(header).toBeVisible();
      await expect(header).toHaveText(rooms);
      // Small capitals in the primary colour, on the grey row.
      await expect(header).toHaveCSS('text-transform', 'uppercase');
    }
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

test.describe('booking with the square button', () => {
  // An adult of the seed without children (internal/seed/demo.go,
  // demoHouseholds; emails are given.family@host): she books for herself
  // only, so her cards book, cancel and join directly.
  const member = { name: 'Irene Inkwell', email: 'irene.inkwell@example.com' };

  test('books a workshop with "Book" and cancels it with "Cancel"', async ({ page }) => {
    await gotoDemo(page, '/login');
    // A first visit has no cached theme: the page shows in Programme until the
    // school's settings arrive, then remounts in Werkstatt (ThemeProvider), and
    // what was typed before is gone. Type once the school's look is on.
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'werkstatt');
    const email = page.getByLabel('Email');
    await email.fill(member.email);
    await page.getByLabel('Password').fill('demo-password');
    await expect(email).toHaveValue(member.email);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('banner').getByRole('button', { name: member.name })).toBeVisible();
    await gotoDemo(page, '/schedule');

    // Workshops are published two to four weeks ahead: far from any cutoff,
    // and nobody of the demo's classes is booked into them automatically.
    await page.getByRole('button', { name: 'Next 14 days' }).click();
    const main = page.getByRole('main');
    const book = page.getByRole('button', { name: 'Book', exact: true });
    const free = main
      .getByRole('group', { name: /\d{2}:\d{2}–\d{2}:\d{2}, Workshop$/ })
      .filter({ has: book })
      .first();
    await expect(free).toBeVisible();
    const label = (await free.getAttribute('aria-label')) ?? '';
    expect(label).not.toBe('');
    const card = main.getByRole('group', { name: label, exact: true });
    const star = card.getByRole('img', { name: 'Booked' });
    await expect(star).toHaveCount(0);

    await card.getByRole('button', { name: 'Book', exact: true }).click();
    await expect(page.getByText(`${member.name} is booked.`)).toBeVisible();
    await expect(star).toBeVisible();
    await expect(card.getByRole('button', { name: 'Book', exact: true })).toHaveCount(0);

    // Canceling asks first, as the sheet does.
    await card.getByRole('button', { name: 'Cancel', exact: true }).click();
    const confirm = page.getByRole('alertdialog', { name: 'Cancel this booking?' });
    await confirm.getByRole('button', { name: 'Cancel booking' }).click();
    await expect(confirm).toBeHidden();
    await expect(page.getByText(`The booking of ${member.name} has been canceled.`)).toBeVisible();
    await expect(star).toHaveCount(0);
    await expect(card.getByRole('button', { name: 'Book', exact: true })).toBeVisible();
  });
});

test("home lists the member's booked lessons and opens one in the schedule", async ({ page }) => {
  // The demo member's family is in several weekly classes on different
  // days (internal/seed/demo.go), so the coming week always has some.
  await signIn(page, 'Member');
  expect(new URL(page.url()).hash).toBe('#/');

  const mine = page.getByRole('region', { name: 'My schedule' });
  const card = mine
    .getByRole('group')
    .filter({ has: page.getByRole('img', { name: 'Booked' }) })
    .first();
  await expect(card).toBeVisible();
  const label = (await card.getAttribute('aria-label')) ?? '';
  const course = label.slice(0, label.indexOf(','));
  expect(course).not.toBe('');

  await card.getByRole('button', { name: label, exact: true }).click();

  expect(new URL(page.url()).hash).toMatch(/^#\/schedule\?/);
  await expect(page.getByRole('dialog').getByRole('heading', { name: course })).toBeVisible();
});

test('the account page saves a phone number', async ({ page }) => {
  const name = await signIn(page, 'Member');
  const openAccount = async () => {
    await page.getByRole('banner').getByRole('button', { name }).click();
    await page.getByRole('menuitem', { name: 'Account settings' }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'My details' })).toBeVisible();
  };
  await openAccount();

  const card = page.getByRole('region', { name: 'Your details' });
  await card.getByLabel('Phone (optional)').fill('+49 000 0000199');
  await card.getByRole('button', { name: 'Save details' }).click();
  await expect(page.getByText('Your details have been saved.')).toBeVisible();

  // Read again from the backend after a visit elsewhere.
  await gotoDemo(page, '/schedule');
  await openAccount();
  await expect(card.getByLabel('Phone (optional)')).toHaveValue('+49 000 0000199');
});

test('the office marks a participant present in the table', async ({ page, isMobile }) => {
  await signIn(page, 'Office');
  await gotoDemo(page, '/schedule');
  await page.getByRole('button', { name: 'Previous 14 days' }).click();
  const main = page.getByRole('main');
  await expect(lessons(main).first()).toBeVisible();

  // Every ballet class of the seed has members, booked into its past
  // lessons, and nobody took the attendance of the ballet ones.
  await main
    .getByRole('button', { name: /^Classical Ballet, / })
    .first()
    .click();
  const sheet = page.getByRole('dialog', { name: 'Classical Ballet' });
  const table = sheet
    .getByRole('region', { name: 'Participants' })
    .getByRole('table', { name: 'Booked participants' });
  const present = table.getByRole('checkbox', { name: / attended$/, checked: false }).first();
  await expect(present).toBeVisible();
  if (!isMobile) {
    // The sheet widens for the table (werkstatt.css): every column shows
    // without scrolling sideways. On a phone the table scrolls.
    expect(
      await table.evaluate((el) => {
        const box = el.parentElement ?? el;
        return box.scrollWidth <= box.clientWidth;
      }),
    ).toBe(true);
  }
  const label = (await present.getAttribute('aria-label')) ?? '';
  const name = label.replace(/ attended$/, '');
  expect(name).not.toBe('');

  await present.click();

  await expect(page.getByText(`${name} attended.`)).toBeVisible();
  await expect(table.getByRole('checkbox', { name: `${name} attended` })).toBeChecked();
  await expect(
    table.getByRole('button', { name: `Reset the attendance of ${name}` }),
  ).toBeVisible();
});
