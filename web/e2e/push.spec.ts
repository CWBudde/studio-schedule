import { expect, gotoDemo, type Page, signIn, signOut, test } from './fixtures';

// Web push in the demo (ADR 0017): its backend has no push service to send
// through, so the page shows the notification through its service worker,
// like a real push. Notifications need the new headless Chromium (the
// headless shell has none), with the permission granted.
test.use({ channel: 'chromium', permissions: ['notifications'] });

async function serviceWorkerReady(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await navigator.serviceWorker.ready;
  });
}

test('a waiting-list invite shows a notification on the device that turned them on', async ({
  page,
}) => {
  await gotoDemo(page, '/');
  await serviceWorkerReady(page);

  // The office adds three places to the full tap workshop: the two
  // waiting are invited, one place is free.
  const office = await signIn(page, 'Office');
  const nav = page.getByRole('navigation', { name: 'Main' });
  await nav.getByRole('link', { name: 'Schedule' }).click();
  const workshop = page
    .getByRole('main')
    .getByRole('button', { name: /^Tap, .+, 13:30–15:00, Workshop$/ })
    .first();
  await workshop.click();
  let sheet = page.getByRole('dialog', { name: 'Tap' });
  await sheet.getByRole('button', { name: 'Edit' }).click();
  const form = page.getByRole('dialog', { name: 'Edit lesson' });
  await form.getByLabel('Capacity').fill('7');
  await form.getByRole('button', { name: 'Save changes' }).click();
  await expect(form).toBeHidden();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await signOut(page, office);

  // Clara turns notifications on for this device.
  const member = await signIn(page, 'Member');
  await page.goto('#/account');
  const card = page.getByRole('region', { name: 'Notifications on this device' });
  const toggle = card.getByRole('switch', { name: 'Notifications on this device' });
  await expect(toggle).not.toBeChecked();
  await toggle.click();
  await expect(toggle).toBeChecked();
  await expect(card.getByText(/This device gets a notification/)).toBeVisible();

  // She takes the free place and puts Ella on the waiting list …
  await nav.getByRole('link', { name: 'Schedule' }).click();
  await workshop.click();
  sheet = page.getByRole('dialog', { name: 'Tap' });
  const booking = sheet.getByRole('region', { name: 'Booking' });
  await booking.getByRole('combobox').click();
  await page.getByRole('option', { name: member }).click();
  await booking.getByRole('button', { name: 'Book', exact: true }).click();
  await expect(page.getByText(`${member} is booked.`)).toBeVisible();
  await booking.getByRole('button', { name: 'Join the waiting list' }).click();
  await expect(page.getByText(/is on the waiting list, position 1\./)).toBeVisible();

  // … and cancels her own booking: Ella is invited, and the device says so.
  await booking.getByRole('button', { name: `Cancel the booking of ${member}` }).click();
  await page
    .getByRole('alertdialog', { name: 'Cancel this booking?' })
    .getByRole('button', { name: 'Cancel booking' })
    .click();
  await expect(page.getByText(`The booking of ${member} has been canceled.`)).toBeVisible();

  await expect
    .poll(
      () =>
        page.evaluate(async () => {
          const reg = await navigator.serviceWorker.getRegistration();
          const shown = (await reg?.getNotifications()) ?? [];
          return shown.map((n) => `${n.title} | ${(n.data as { url?: string }).url ?? ''}`);
        }),
      { timeout: 30_000 },
    )
    .toContainEqual(expect.stringMatching(/^A place is free: Tap \| .+waitlist\/accept\?token=/));

  // Signing out turns them off here.
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();
  await signOut(page, member);
  expect(await page.evaluate(() => localStorage.getItem('studio-schedule-demo-push'))).toBeNull();
});
