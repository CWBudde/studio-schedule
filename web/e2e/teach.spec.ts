import { expect, gotoDemo, lessons, type Page, test } from './fixtures';

// The demo seed has a ballet workshop of Alina Starling (the demo's
// instructor login) 24 days ahead, 13:30–15:30, with a guest with an email
// (Mara Gastfield), a walk-in guest without one (Pia Walkby) and Ute
// Tallowfield on a trial. Alina's weekly ballet classes run every week; the
// past week's have the bookings of their members, whose attendance nobody
// took. Nina Tapworth teaches jazz, barre fitness, tap and hip-hop.

type Role = 'Instructor' | 'Office';

/**
 * Signs in with the demo account of a role (from the login page, which the
 * header's "Sign in" opens) and opens the instructor view from the main
 * navigation; returns the account's name.
 */
async function openTeachAs(page: Page, role: Role): Promise<string> {
  await page.getByRole('banner').getByRole('link', { name: 'Sign in' }).click();
  const account = page
    .getByRole('region', { name: 'Demo accounts' })
    .getByRole('button', { name: new RegExp(`^Sign in as .+ \\(${role}\\)$`) });
  const label = (await account.getAttribute('aria-label')) ?? '';
  const name = new RegExp(`^Sign in as (.+) \\(${role}\\)$`).exec(label)?.[1] ?? '';
  expect(name).not.toBe('');
  await account.click();
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Teaching' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Teaching' })).toBeVisible();
  return name;
}

test('the instructor sees her lessons and messages the participants of one', async ({ page }) => {
  await gotoDemo(page, '/');
  const name = await openTeachAs(page, 'Instructor');
  const main = page.getByRole('main');

  // Today's range: her classes, today marked; the past week folded away,
  // with the attendance nobody took.
  await expect(lessons(main).first()).toBeVisible();
  await expect(main.getByText('Instructor', { exact: true }).first()).toBeVisible();
  const earlier = main.getByRole('button', {
    name: /^Earlier: \d+ lessons? · \d+ without attendance$/,
  });
  await expect(earlier).toHaveAttribute('aria-expanded', 'false');
  await earlier.click();
  await expect(earlier).toHaveAttribute('aria-expanded', 'true');
  await expect(main.getByText('Attendance missing').first()).toBeVisible();

  // Three weeks ahead: the workshop with her guests and the trial.
  await main.getByRole('button', { name: 'Next 2 weeks' }).click();
  await main.getByRole('button', { name: 'Next 2 weeks' }).click();
  const row = main
    .getByRole('listitem')
    .filter({
      has: page.getByRole('button', { name: /^Classical Ballet, .+, 13:30–15:30, Workshop/ }),
    })
    .first();
  const workshop = row.getByRole('button', { name: /^Classical Ballet, / });
  await expect(row.getByText('3/16 booked · 1 trial · 2 guests')).toBeVisible();
  await expect(row.getByText('Instructor', { exact: true })).toBeVisible();
  // Nothing is wider than the page, on a phone too.
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true);

  // It opens in the lesson sheet, with its participants.
  await workshop.click();
  const sheet = page.getByRole('dialog', { name: 'Classical Ballet' });
  const participants = sheet.getByRole('region', { name: 'Participants' });
  await expect(participants.getByText(/^3 of 16 booked · 13 places free$/)).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();

  // A message to them, which reaches the guest with an address.
  await row.getByRole('button', { name: /^Message participants of Classical Ballet, / }).click();
  const dialog = page.getByRole('dialog', { name: 'Message participants' });
  const message = 'Please bring your character shoes on Saturday. 🩰';
  await dialog.getByRole('textbox', { name: 'Message' }).fill(message);
  await expect(dialog.getByText('48/1000')).toBeVisible();
  await dialog.getByRole('button', { name: 'Send' }).click();
  await expect(dialog).toBeHidden();
  // Mara has an address; Pia has none, and neither has Ute (no account, no guardian).
  await expect(page.getByText('Sent to 1 recipient.')).toBeVisible();

  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox
    .getByRole('article')
    .filter({ hasText: /Hello Mara/ })
    .filter({ hasText: message });
  await expect(mail).toHaveCount(1);
  await expect(mail.getByRole('heading', { name: /Message about Classical Ballet/ })).toBeVisible();
  await expect(mail.getByText('mara.gastfield@example.com')).toBeVisible();
  await expect(
    mail.getByText(`${name} sends a message about this lesson`, { exact: false }),
  ).toBeVisible();
  // Replies go to Alina, not to the school's no-reply address.
  await expect(mail.getByText('Reply to', { exact: true })).toBeVisible();
  await expect(mail.getByText(/^alina\.starling@example\.net$/)).toBeVisible();
});

test('the office sees every lesson and narrows them to one instructor', async ({ page }) => {
  await gotoDemo(page, '/');
  await openTeachAs(page, 'Office');
  const main = page.getByRole('main');

  await expect(main.getByText(/^Every lesson of the school/)).toBeVisible();
  await expect(lessons(main).first()).toBeVisible();
  // The office teaches none of them.
  await expect(main.getByText('Instructor', { exact: true })).toHaveCount(0);
  await expect(
    main.getByRole('listitem').filter({ hasText: 'Alina Starling' }).first(),
  ).toBeVisible();

  await main.getByRole('combobox', { name: 'Staff' }).click();
  await page.getByRole('option', { name: 'Nina Tapworth' }).click();
  await expect(main.getByRole('combobox', { name: 'Staff' })).toHaveText('Nina Tapworth');
  // Only her lessons are left: jazz, barre, tap and hip-hop, all hers.
  const rows = main.getByRole('listitem');
  await expect(rows.filter({ hasText: 'Alina Starling' })).toHaveCount(0);
  await expect(rows.first()).toBeVisible();
  await expect(rows.filter({ hasNotText: 'Nina Tapworth' })).toHaveCount(0);
  await expect(main.getByRole('button', { name: /^Tap, / }).first()).toBeVisible();
});
