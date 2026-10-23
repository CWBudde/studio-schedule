import { expect, gotoDemo, hashSearch, test } from './fixtures';

test('keeps the route params when the link has a real query string', async ({ page }) => {
  // Tracking params land before the hash, e.g. when a link is shared.
  await gotoDemo(page, '?fbclid=e2e#/schedule?view=week&level=beginner');

  await expect(page.getByRole('radio', { name: 'Week' })).toBeChecked();
  await expect(page.getByRole('combobox', { name: 'Level' })).toHaveText('Beginner');
  expect(new URL(page.url()).search).toBe('');
  expect(hashSearch(page).get('view')).toBe('week');
  expect(hashSearch(page).get('level')).toBe('beginner');
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
