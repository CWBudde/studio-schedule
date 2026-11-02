import { expect, gotoDemo, hashSearch, lessons, test } from './fixtures';

test('keeps the route params when the link has a real query string', async ({ page }) => {
  // Tracking params land before the hash, e.g. when a link is shared.
  await gotoDemo(page, '?fbclid=e2e#/schedule?view=week&from=2026-10-26');

  await expect(page.getByRole('radio', { name: 'Week' })).toBeChecked();
  await expect(page.getByRole('heading', { name: 'Oct 26 – Nov 1, 2026' })).toBeVisible();
  expect(new URL(page.url()).search).toBe('');
  expect(hashSearch(page).get('view')).toBe('week');
  expect(hashSearch(page).get('from')).toBe('2026-10-26');
});

test("drops an old bookmark's fixed level", async ({ page }) => {
  // Levels were a fixed set before they became the school's own (ADR 0021).
  await gotoDemo(page, '/schedule?level=beginner');

  await expect(page.getByRole('combobox', { name: 'Level' })).toHaveText('Any level');
  await expect(lessons(page.getByRole('main')).first()).toBeVisible();
});

test('opens a linked lesson', async ({ page }) => {
  // Find a lesson id through the UI, then follow a link to it. The demo's
  // lesson IDs are random per visit, so the link stays within this one: a
  // hash change, not a reload.
  await gotoDemo(page, '/schedule');
  await page
    .getByRole('main')
    .getByRole('button', { name: /\d{2}:\d{2}–/ })
    .first()
    .click();
  const lesson = hashSearch(page).get('lesson') ?? '';
  expect(lesson).not.toBe('');

  await gotoDemo(page, '/');
  await gotoDemo(page, `/schedule?lesson=${lesson}`);

  await expect(page.getByRole('dialog').getByText('Capacity', { exact: true })).toBeVisible();
});

test('says when a linked lesson is not available', async ({ page }) => {
  await gotoDemo(page, '/schedule?lesson=0190a000-0000-7000-8000-000000000000');

  await expect(
    page.getByRole('dialog').getByRole('heading', { name: 'This lesson is not available' }),
  ).toBeVisible();
});
