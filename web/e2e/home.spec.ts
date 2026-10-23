import { expect, gotoDemo, hashSearch, test } from './fixtures';

test('the demo starts its in-browser backend', async ({ page }) => {
  await gotoDemo(page);

  await expect(page.getByRole('complementary', { name: 'Demo mode' })).toBeVisible();
  const status = page.getByRole('region', { name: 'System status' });
  await expect(status.getByText('Backend: OK')).toBeVisible();
  await expect(status.getByText('Europe/Berlin')).toBeVisible();
});

test('the next lessons link to their details in the schedule', async ({ page }) => {
  await gotoDemo(page);

  const teaser = page.getByRole('region', { name: 'Next lessons' });
  const next = teaser.getByRole('listitem').getByRole('link');
  await expect(next.first()).toBeVisible();
  const courseType = (await next.first().locator('span').first().textContent()) ?? '';
  expect(courseType).not.toBe('');

  await next.first().click();

  expect(new URL(page.url()).hash).toMatch(/^#\/schedule\?/);
  const sheet = page.getByRole('dialog');
  await expect(sheet.getByRole('heading', { name: courseType })).toBeVisible();
  expect(hashSearch(page).get('lesson')).toMatch(/^[0-9a-f-]{36}$/);
  expect(hashSearch(page).get('from')).toMatch(/^\d{4}-\d{2}-\d{2}$/);
});

test('the header links to the schedule', async ({ page }) => {
  await gotoDemo(page);

  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();

  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Today' })).toBeDisabled();
});
