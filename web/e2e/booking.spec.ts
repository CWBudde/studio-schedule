import { expect, type Page, signIn, test } from './fixtures';

/** Signs in with the demo member and opens the schedule; returns the name. */
async function signInAsMember(page: Page): Promise<string> {
  const name = await signIn(page, 'Member');
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
  return name;
}

test('a member books a workshop, gets the mail and cancels again', async ({ page }) => {
  const name = await signInAsMember(page);

  // Workshops are published two to four weeks ahead: far from any cutoff,
  // and nobody of the demo's classes is booked into them automatically.
  await page.getByRole('button', { name: 'Next 14 days' }).click();
  const main = page.getByRole('main');
  const card = main
    .getByRole('button', { name: /\d{2}:\d{2}–\d{2}:\d{2}, Workshop$/ })
    .filter({ hasNotText: 'Booked' })
    .filter({ hasText: /\d+ free/ })
    .first();
  await expect(card).toBeVisible();
  const cardName = (await card.getAttribute('aria-label')) ?? '';
  const courseType = cardName.split(', ')[0] ?? '';
  const free = Number(/(\d+) free/.exec((await card.textContent()) ?? '')?.[1]);
  expect(free).toBeGreaterThan(0);
  const named = main.getByRole('button', { name: cardName, exact: true });

  await card.click();
  const sheet = page.getByRole('dialog', { name: courseType });
  const booking = sheet.getByRole('region', { name: 'Booking' });
  await expect(booking.getByText(`${free} places left`)).toBeVisible();
  await booking.getByRole('button', { name: 'Book', exact: true }).click();

  await expect(page.getByText(`${name} is booked.`)).toBeVisible();
  await expect(booking.getByText('Booked', { exact: true })).toBeVisible();
  await expect(
    booking.getByText(free - 1 === 1 ? '1 place left' : `${free - 1} places left`),
  ).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();
  await expect(named).toContainText('Booked');
  await expect(named).toContainText(`${free - 1} free`);

  // The confirmation mail is in the demo mailbox.
  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox
    .getByRole('article')
    .filter({ hasText: new RegExp(`${courseType} on .+ is booked`) });
  await expect(mail).toHaveCount(1);
  await expect(mail).toContainText(`Booked for: ${name}`);
  await page.keyboard.press('Escape');
  await expect(mailbox).toBeHidden();

  // Cancel: after asking, the place is free again.
  await named.click();
  await booking.getByRole('button', { name: `Cancel the booking of ${name}` }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Cancel this booking?' });
  await confirm.getByRole('button', { name: 'Cancel booking' }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText(`The booking of ${name} has been canceled.`)).toBeVisible();
  await expect(booking.getByText(`${free} places left`)).toBeVisible();
  await expect(booking.getByRole('button', { name: 'Book', exact: true })).toBeEnabled();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(named).not.toContainText('Booked');
  await expect(named).toContainText(`${free} free`);
});
