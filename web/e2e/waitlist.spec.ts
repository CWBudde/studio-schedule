import type { Locator } from '@playwright/test';
import { expect, gotoDemo, type Page, signIn, signOut, test } from './fixtures';

// The demo seed has a small tap workshop ten days ahead (13:30–15:00, in
// the schedule's first 14 days) that four members booked up, with two others on its waiting list (none of the
// demo logins' families). See ADR 0010.

/**
 * Signs in with the demo account of a role (from the login page, which the
 * header's "Sign in" opens) and opens the schedule; returns the account's name.
 */
async function signInAs(page: Page, role: 'Member' | 'Office'): Promise<string> {
  const name = await signIn(page, role);
  await openSchedule(page);
  return name;
}

async function openSchedule(page: Page): Promise<void> {
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
}

/** The card of the seed's full tap workshop. */
const workshopCard = (page: Page): Locator =>
  page
    .getByRole('main')
    .getByRole('button', { name: /^Tap, .+, 13:30–15:00, Workshop$/ })
    .first();

/** Opens the workshop's sheet and returns it with its booking section. */
async function openWorkshop(page: Page): Promise<{ sheet: Locator; booking: Locator }> {
  await workshopCard(page).click();
  const sheet = page.getByRole('dialog', { name: 'Tap' });
  return { sheet, booking: sheet.getByRole('region', { name: 'Booking' }) };
}

test('a member joins the waiting list, is invited when places free and accepts from the mail', async ({
  page,
}) => {
  await gotoDemo(page, '/');
  const member = await signInAs(page, 'Member');
  const givenName = member.split(' ')[0] ?? '';

  // The workshop is full: the member joins its waiting list, behind the two there.
  await expect(workshopCard(page)).toContainText('Full');
  let { sheet, booking } = await openWorkshop(page);
  await expect(booking.getByText('This lesson is full.')).toBeVisible();
  await booking.getByRole('checkbox', { name: 'Stay on the list if I miss an invite' }).check();
  await booking.getByRole('button', { name: 'Join the waiting list' }).click();
  await expect(page.getByText(`${member} is on the waiting list, position 3.`)).toBeVisible();
  await expect(booking.getByText('Waiting, position 3')).toBeVisible();
  await expect(
    booking.getByRole('button', { name: `Leave the waiting list for ${member}` }),
  ).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();
  await expect(workshopCard(page)).toContainText('Waiting list');
  await signOut(page, member);

  // The office adds three places: one invite for each of the three waiting.
  const office = await signInAs(page, 'Office');
  ({ sheet } = await openWorkshop(page));
  await expect(sheet.getByRole('region', { name: 'Manage this lesson' })).toBeVisible();
  await sheet.getByRole('button', { name: 'Edit' }).click();
  const form = page.getByRole('dialog', { name: 'Edit lesson' });
  await expect(form.getByLabel('Capacity')).toHaveValue('4');
  await form.getByLabel('Capacity').fill('7');
  await form.getByRole('button', { name: 'Save changes' }).click();
  await expect(form).toBeHidden();
  await expect(page.getByText('The lesson has been saved.')).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();
  await signOut(page, office);

  // The member sees the invite in the app …
  await signInAs(page, 'Member');
  await expect(workshopCard(page)).toContainText('Invited');
  ({ sheet, booking } = await openWorkshop(page));
  await expect(booking.getByText(/^A place is reserved for you until .+\.$/)).toBeVisible();
  await expect(
    booking.getByRole('button', { name: `Accept the place for ${member}` }),
  ).toBeEnabled();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();

  // … and in the mailbox, whose link opens the confirm page.
  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox
    .getByRole('article')
    .filter({ hasText: new RegExp(`Hello ${givenName},`) })
    .filter({ hasText: /a place in Tap on .+ is free/ });
  await expect(mail).toHaveCount(1);
  await mail.getByRole('link', { name: /waitlist\/accept\?token=/ }).click();
  await expect(mailbox).toBeHidden();

  await expect(page.getByRole('heading', { level: 1, name: 'Waiting list invite' })).toBeVisible();
  await expect(page.getByRole('heading', { level: 2, name: 'Tap' })).toBeVisible();
  await expect(page.getByText(/, 13:30–15:00$/)).toBeVisible();
  await expect(
    page.getByText(`A place is reserved for ${givenName} until`, { exact: false }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Accept the place' }).click();
  await expect(
    page.getByRole('heading', { level: 1, name: 'Booked — see you there' }),
  ).toBeVisible();

  // Booked: the schedule says so.
  await page.getByRole('link', { name: 'To the schedule' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
  await expect(workshopCard(page)).toContainText('Booked');
  await expect(workshopCard(page)).not.toContainText('Invited');
});

test('an unknown invite link says so and books nothing', async ({ page }) => {
  await gotoDemo(page, '/waitlist/accept?token=not-a-real-token');

  await expect(page.getByRole('heading', { level: 1, name: 'Unknown link' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Accept the place' })).toHaveCount(0);
});
