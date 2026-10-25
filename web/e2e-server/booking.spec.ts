import { accounts, expect, signIn, test } from './fixtures';

test('a member signs in, cancels a lesson of her class and books it again', async ({ page }) => {
  const { name } = accounts.member;
  await signIn(page, accounts.member);
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();

  // The class members are booked into their lessons. The last one of the
  // next 14 days is far from the cutoffs.
  const main = page.getByRole('main');
  const card = main
    .getByRole('button', { name: /\d{2}:\d{2}–\d{2}:\d{2}/ })
    .filter({ hasText: 'Booked' })
    .filter({ hasText: /\d+ free/ })
    .last();
  await expect(card).toBeVisible();
  const cardName = (await card.getAttribute('aria-label')) ?? '';
  const courseType = cardName.split(', ')[0] ?? '';
  const free = Number(/(\d+) free/.exec((await card.textContent()) ?? '')?.[1]);
  const named = main.getByRole('button', { name: cardName, exact: true });

  await card.click();
  const sheet = page.getByRole('dialog', { name: courseType });
  const booking = sheet.getByRole('region', { name: 'Booking' });
  await booking.getByRole('button', { name: `Cancel the booking of ${name}` }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Cancel this booking?' });
  await confirm.getByRole('button', { name: 'Cancel booking' }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText(`The booking of ${name} has been canceled.`)).toBeVisible();
  await expect(booking.getByText(`${free + 1} places left`)).toBeVisible();

  // Canceled in the database, not only in the page.
  await sheet.getByRole('button', { name: 'Close' }).click();
  await page.reload();
  await expect(named).not.toContainText('Booked');
  await expect(named).toContainText(`${free + 1} free`);

  // Taking the date back is an ordinary booking.
  await named.click();
  await booking.getByRole('button', { name: 'Book', exact: true }).click();
  await expect(page.getByText(`${name} is booked.`)).toBeVisible();
  await expect(booking.getByText('Booked', { exact: true })).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await page.reload();
  await expect(named).toContainText('Booked');
  await expect(named).toContainText(`${free} free`);
});
