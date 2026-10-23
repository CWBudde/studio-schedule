import { expect, gotoDemo, hashSearch, type Page, test } from './fixtures';

/** Class cards: articles named "<course>, <weekday>, <hh:mm–hh:mm>". */
function classCards(page: Page) {
  return page.getByRole('main').getByRole('article', { name: /, \d{2}:\d{2}–\d{2}:\d{2}$/ });
}

test('lists the weekly classes of the current term, Monday first', async ({ page }) => {
  await gotoDemo(page, '/classes');

  await expect(page.getByRole('heading', { level: 1, name: 'Classes' })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Term' })).toHaveText(/^School Year /);

  const main = page.getByRole('main');
  const days = main.getByRole('heading', { level: 2 });
  await expect(days.first()).toHaveText('Monday');
  const names = await days.allTextContents();
  // Monday to Saturday in calendar order, each day once.
  const week = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];
  expect(names.map((d) => week.indexOf(d))).toEqual(
    [...names.map((d) => week.indexOf(d))].sort((a, b) => a - b),
  );
  expect(new Set(names).size).toBe(names.length);

  const monday = main.getByRole('region', { name: 'Monday' });
  const first = monday.getByRole('article').first();
  await expect(first).toHaveAccessibleName(/^.+, Monday, \d{2}:\d{2}–\d{2}:\d{2}$/);
  await expect(first.getByRole('heading', { level: 3 })).toBeVisible();
  await expect(main.getByText(/^accompanied by /).first()).toBeVisible();
  expect(await classCards(page).count()).toBeGreaterThan(5);
});

test('is linked from the header and offers the terms', async ({ page }) => {
  await gotoDemo(page, '/');

  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Classes' })
    .click();

  await expect(page.getByRole('heading', { level: 1, name: 'Classes' })).toBeVisible();
  const term = page.getByRole('combobox', { name: 'Term' });
  await expect(term).toHaveText(/^School Year /);
  const termName = (await term.textContent()) ?? '';
  await expect(classCards(page).first()).toBeVisible();

  // The list offers the demo's terms, the shown one selected. (Choosing
  // another term writes it into the URL: see TimetablePage.test.tsx.)
  await term.click();
  const options = page.getByRole('option');
  await expect(page.getByRole('option', { name: termName })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.keyboard.press('Escape');
  await expect(options).toHaveCount(0);
  expect(hashSearch(page).has('term')).toBe(false);
});

test('shows the current term for a link to an unknown term', async ({ page }) => {
  await gotoDemo(page, '/classes?term=0190a000-0000-7000-8000-000000000999');

  await expect(page.getByRole('combobox', { name: 'Term' })).toHaveText(/^School Year /);
  await expect(classCards(page).first()).toBeVisible();
});

for (const lang of ['en', 'de']) {
  test(`fits the screen without scrolling sideways (${lang})`, async ({ page }) => {
    await page.addInitScript((l) => localStorage.setItem('studio-schedule.lang', l), lang);
    await gotoDemo(page, '/classes');
    await expect(classCards(page).first()).toBeVisible();

    // Neither the page nor the header (which clips) is wider than the screen.
    const overflow = await page.evaluate(() =>
      [document.documentElement, document.querySelector('header > div')].map((el) =>
        el ? el.scrollWidth - el.clientWidth : 0,
      ),
    );
    expect(overflow).toEqual([0, 0]);
  });
}
