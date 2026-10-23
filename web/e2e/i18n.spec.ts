import { expect, gotoDemo, lessons, test } from './fixtures';

test('switches the schedule to German', async ({ page }) => {
  await gotoDemo(page, '/schedule');
  await expect(lessons(page.getByRole('main')).first()).toBeVisible();

  await page.getByRole('button', { name: 'Change language' }).click();
  await page.getByRole('menuitemradio', { name: 'Deutsch' }).click();

  await expect(page.getByRole('heading', { level: 1, name: 'Stundenplan' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Stundenplan' })).toBeVisible();
  await expect(page.getByRole('radio', { name: 'Woche' })).toBeVisible();
  await expect(page.getByRole('main').getByRole('heading', { level: 2 }).first()).toHaveText(
    /^(Montag|Dienstag|Mittwoch|Donnerstag|Freitag|Samstag|Sonntag), /,
  );
  await expect(page.getByRole('timer')).toHaveText(/^Nächste Freigabe in /);
});
