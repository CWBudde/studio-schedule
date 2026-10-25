import { expect, gotoDemo, signIn, test } from './fixtures';

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

test('a member books a workshop, finds it in My lessons and cancels it there', async ({ page }) => {
  const name = await signIn(page, 'Member');
  const nav = page.getByRole('navigation', { name: 'Main' });

  // Book a workshop from the schedule: two to four weeks ahead, far from
  // any cutoff, and nobody of the demo's classes is booked into it.
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

  // My lessons lists it under "Coming up": the card's lesson, perhaps with the person's name.
  await nav.getByRole('link', { name: 'My lessons' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'My lessons' })).toBeVisible();
  const upcoming = page.getByRole('region', { name: 'Coming up' });
  const lesson = new RegExp(`^${escape(cardName.replace(/, Workshop$/, ''))}(, .+)?$`);
  const row = upcoming
    .getByRole('listitem')
    .filter({ has: page.getByRole('button', { name: lesson }) });
  await expect(row).toHaveCount(1);
  await expect(row.getByText('Booked', { exact: true })).toBeVisible();
  await expect(row.getByText('Workshop', { exact: true })).toBeVisible();
  // Nothing is wider than the page, on a phone too.
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true);

  // It opens in the lesson sheet.
  await row.getByRole('button', { name: lesson }).click();
  await expect(sheet.getByRole('region', { name: 'Booking' }).getByText('Booked')).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();

  // Canceled from here, after asking, it is gone from "Coming up".
  await row.getByRole('button', { name: `Cancel the booking of ${name}` }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Cancel this booking?' });
  await expect(confirm.getByText('This counts as a late cancellation.')).toHaveCount(0);
  await confirm.getByRole('button', { name: 'Cancel booking' }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText(`The booking of ${name} has been canceled.`)).toBeVisible();
  await expect(row).toHaveCount(0);
});

test('visitors are asked to sign in', async ({ page }) => {
  await gotoDemo(page, '/my-lessons');

  await expect(page.getByRole('heading', { level: 1, name: 'My lessons' })).toBeVisible();
  await expect(page.getByText('Sign in to see your lessons')).toBeVisible();
  await expect(
    page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'My lessons' }),
  ).toHaveCount(0);
});
