import { addDays, expect, gotoDemo, hashSearch, lessons, type Page, test } from './fixtures';

/**
 * Opens the list on the 14 days before today. How much of the coming days is
 * released depends on the weekday (on Sunday before the 18:00 release, only
 * the early workshops are), but the past days of the demo term always are.
 */
async function gotoPastFortnight(page: Page): Promise<void> {
  await gotoDemo(page, '/schedule');
  await page.getByRole('button', { name: 'Previous 14 days' }).click();
  await expect(lessons(page.getByRole('main')).first()).toBeVisible();
}

test.describe('list view', () => {
  test('shows the released lessons of the next 14 days', async ({ page }) => {
    await gotoDemo(page, '/schedule');

    const main = page.getByRole('main');
    await expect(lessons(main).first()).toBeVisible();
    await expect(page.getByRole('button', { name: 'Today' })).toBeDisabled();
    await expect(main.getByText(/^Classes are published through /)).toBeVisible();
  });

  test('groups the lessons by day', async ({ page }) => {
    await gotoPastFortnight(page);

    // Day headings (h2) group the cards; the page heading is the only h1.
    const main = page.getByRole('main');
    expect(await main.getByRole('heading', { level: 2 }).count()).toBeGreaterThan(1);
    await expect(main.getByText(/^Classes are published through /)).toBeHidden();
  });

  test('shows workshops beyond the release window', async ({ page }) => {
    await gotoDemo(page, '/schedule');
    await expect(lessons(page.getByRole('main')).first()).toBeVisible();

    await page.getByRole('button', { name: 'Next 14 days' }).click();

    // Regular classes are released at most a week ahead; the demo seed puts
    // workshops 17 and 24 days after today, published early.
    const cards = lessons(page.getByRole('main'));
    await expect(cards).toHaveCount(2);
    for (const card of await cards.all()) {
      await expect(card).toHaveAccessibleName(/, Workshop$/);
    }
    await expect(page.getByText(/^Classes are published through /)).toBeVisible();
    await expect(page.getByRole('button', { name: 'Today' })).toBeEnabled();
  });

  test('counts down to the next release', async ({ page }) => {
    await page.clock.install();
    await gotoDemo(page, '/schedule');

    const timer = page.getByRole('timer');
    await expect(timer).toHaveText(/^Next release in /);
    const before = (await timer.textContent()) ?? '';

    // Skip one step of the smallest unit shown, so the jump never reaches the
    // release itself (the backend's clock in the Worker is not faked).
    const step = /day/.test(before) ? '01:00:00' : /hour/.test(before) ? '01:00' : '00:02';
    await page.clock.fastForward(step);

    await expect(timer).not.toHaveText(before);
  });

  test('filters by course type and resets', async ({ page }) => {
    await gotoPastFortnight(page);
    const main = page.getByRole('main');
    const all = await lessons(main).count();

    // Pick the course type of the first card.
    const first = (await lessons(main).first().getAttribute('aria-label')) ?? '';
    const courseType = first.split(',')[0] ?? '';
    await page.getByRole('combobox', { name: 'Course type' }).click();
    await page.getByRole('option', { name: courseType, exact: true }).click();

    await expect(page.getByRole('combobox', { name: 'Course type' })).toHaveText(courseType);
    expect(hashSearch(page).get('courseType')).toMatch(/^[0-9a-f-]{36}$/);
    await expect(async () => {
      const names = await lessons(main).evaluateAll((els) =>
        els.map((el) => el.getAttribute('aria-label') ?? ''),
      );
      expect(names.length).toBeGreaterThan(0);
      expect(names.length).toBeLessThan(all);
      for (const name of names) expect(name.startsWith(`${courseType},`)).toBe(true);
    }).toPass();

    await page.getByRole('button', { name: 'Reset filters' }).click();

    expect(hashSearch(page).has('courseType')).toBe(false);
    expect(hashSearch(page).has('from')).toBe(true);
    await expect(lessons(main)).toHaveCount(all);
  });

  test('filters by level: a span through it and all levels match', async ({ page }) => {
    await gotoPastFortnight(page);
    const main = page.getByRole('main');
    // Creative Dance states only an age (4–6), the Thursday Tap class one level (Beginner).
    const creativeDance = main.getByRole('button', { name: /^Creative Dance, / });
    const beginnerTap = main.getByRole('button', { name: /^Tap, Thursday, / });
    await expect(creativeDance.first()).toBeVisible();
    await expect(creativeDance.first()).toContainText('4–6 yrs');
    await expect(beginnerTap.first()).toBeVisible();

    await page.getByRole('combobox', { name: 'Level' }).click();
    await page.getByRole('option', { name: 'Intermediate', exact: true }).click();

    await expect(page.getByRole('combobox', { name: 'Level' })).toHaveText('Intermediate');
    expect(hashSearch(page).get('level')).toMatch(/^[0-9a-f-]{36}$/);
    // The Thursday ballet class spans Beginner to Intermediate; Barre Fitness is for all levels.
    const spanBallet = main.getByRole('button', {
      name: /^Classical Ballet, Thursday, .*18:30–20:00/,
    });
    await expect(spanBallet.first()).toBeVisible();
    await expect(spanBallet.first()).toContainText('Beginner–Intermediate');
    const allLevels = main.getByRole('button', { name: /^Barre Fitness, Tuesday, / });
    await expect(allLevels.first()).toBeVisible();
    await expect(allLevels.first()).toContainText('All levels');
    await expect(creativeDance).toHaveCount(0);
    await expect(beginnerTap).toHaveCount(0);
  });

  test('opens a lesson in a sheet that closes without a history entry', async ({ page }) => {
    await gotoDemo(page, '/schedule');
    const card = lessons(page.getByRole('main')).first();
    const courseType = ((await card.getAttribute('aria-label')) ?? '').split(',')[0] ?? '';

    await card.click();

    const sheet = page.getByRole('dialog');
    await expect(sheet.getByRole('heading', { name: courseType })).toBeVisible();
    await expect(sheet.getByText('Instructor', { exact: true })).toBeVisible();
    await expect(sheet.getByText('Capacity', { exact: true })).toBeVisible();
    expect(hashSearch(page).get('lesson')).toMatch(/^[0-9a-f-]{36}$/);

    await page.keyboard.press('Escape');

    await expect(sheet).toBeHidden();
    expect(hashSearch(page).has('lesson')).toBe(false);

    // Back leaves the schedule's open-lesson state behind instead of reopening it.
    await page.goBack();
    await expect(page.getByRole('dialog')).toBeHidden();
  });
});

test.describe('week view', () => {
  test('shows Monday to Sunday and moves by weeks', async ({ page }) => {
    await gotoDemo(page, '/schedule');

    await page.getByRole('radio', { name: 'Week' }).click();

    expect(hashSearch(page).get('view')).toBe('week');
    const grid = page.getByRole('group', { name: 'Week timetable' });
    await expect(grid.getByRole('heading', { level: 3 })).toHaveCount(7);
    await expect(grid.getByRole('heading', { level: 3 }).first()).toHaveText(/^Mon /);
    await expect(grid.locator('[aria-current="date"]')).toHaveCount(1);
    await expect(lessons(grid).first()).toBeVisible();
    await expect(page.getByRole('button', { name: 'This week' })).toBeDisabled();

    await page.getByRole('button', { name: 'Next week' }).click();

    const monday = hashSearch(page).get('from') ?? '';
    expect(monday).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(new Date(`${monday}T00:00:00Z`).getUTCDay()).toBe(1);
    await expect(grid.locator('[aria-current="date"]')).toHaveCount(0);

    await page.getByRole('button', { name: 'Previous week' }).click();
    expect(hashSearch(page).get('from')).toBe(addDays(monday, -7));
    await expect(grid.locator('[aria-current="date"]')).toHaveCount(1);
  });

  test('fits the screen: one day per screen on phones, seven on desktops', async ({
    page,
    isMobile,
  }) => {
    await gotoDemo(page, '/schedule?view=week');

    const grid = page.getByRole('group', { name: 'Week timetable' });
    const day = grid.getByRole('region').first();
    const gridBox = await grid.boundingBox();
    const dayBox = await day.boundingBox();
    expect(gridBox && dayBox).toBeTruthy();
    const share = (dayBox?.width ?? 0) / (gridBox?.width ?? 1);
    if (isMobile) expect(share).toBeGreaterThan(0.75);
    else expect(share).toBeLessThan(0.2);
  });
});
