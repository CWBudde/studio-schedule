import { expect, gotoDemo, lessons, type Page, signIn, signOut, test } from './fixtures';

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
  const name = await signIn(page, role);
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Teaching' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Teaching' })).toBeVisible();
  return name;
}

/** Today's date at the school (Europe/Berlin), whatever the browser's zone. */
const schoolToday = () =>
  new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Berlin' }).format(new Date());

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

test('the instructor takes attendance of her lesson that started today', async ({ page }) => {
  // Instructors take attendance from the start of a lesson to the end of its
  // school day, and which of Alina's seeded lessons that is depends on the
  // weekday and the hour the test runs. So the office records a lesson of
  // hers that began at midnight today (a lesson in the past, confirmed).
  await gotoDemo(page, '/');
  const office = await signIn(page, 'Office');
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
  await page.getByRole('button', { name: 'New lesson' }).click();
  const form = page.getByRole('dialog', { name: 'New one-off lesson' });
  await form.getByRole('combobox', { name: 'Course type' }).click();
  await page.getByRole('option', { name: 'Classical Ballet' }).click();
  await form.getByLabel('Date').fill(schoolToday());
  await form.getByLabel('Start time').fill('00:00');
  await form.getByLabel('Duration (minutes)').fill('45');
  await form.getByRole('combobox', { name: 'Room' }).click();
  await page.getByRole('option', { name: 'Studio North · Studio C' }).click();
  await form.getByRole('combobox', { name: 'Instructor' }).click();
  await page.getByRole('option', { name: 'Alina Starling' }).click();
  await form.getByRole('button', { name: 'Add lesson' }).click();
  await form
    .getByRole('alert')
    .filter({ hasText: 'This lesson is in the past' })
    .getByRole('button', { name: 'Save anyway' })
    .click();
  await expect(form).toBeHidden();
  const sheet = page.getByRole('dialog', { name: 'Classical Ballet' });
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();
  await signOut(page, office);

  // Alina finds it among today's lessons; nobody to mark yet.
  await openTeachAs(page, 'Instructor');
  const main = page.getByRole('main');
  const row = main
    .getByRole('listitem')
    .filter({ has: page.getByRole('button', { name: /^Classical Ballet, .+, 00:00–00:45/ }) });
  await expect(row.getByText('0/10 booked')).toBeVisible();
  await row.getByRole('button', { name: /^Classical Ballet, / }).click();
  const section = sheet.getByRole('region', { name: 'Participants' });

  // A walk-in joins, and she marks them as attended.
  await section.getByRole('button', { name: 'Add participant' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add participant' });
  await dialog.getByRole('tab', { name: 'Guest' }).click();
  await dialog.getByRole('textbox', { name: 'Name' }).fill('Gus Visitor');
  await dialog.getByRole('button', { name: 'Add' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('Gus Visitor has been added.')).toBeVisible();

  const attended = section.getByRole('button', { name: 'Gus Visitor attended' });
  await expect(attended).toHaveAttribute('aria-pressed', 'false');
  await attended.click();
  await expect(page.getByText('Gus Visitor attended.')).toBeVisible();
  await expect(attended).toHaveAttribute('aria-pressed', 'true');
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();

  // Nobody is left to mark: the instructor view says so.
  await expect(row.getByText('1/10 booked')).toBeVisible();
  await expect(row.getByText('Attendance taken')).toBeVisible();
});
