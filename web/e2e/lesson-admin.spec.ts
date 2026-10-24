import type { Locator } from '@playwright/test';
import { addDays, expect, gotoDemo, type Page, test } from './fixtures';

// The demo books class members into every lesson of their classes, so these
// tests change a lesson of the one class without members and see "nobody
// gets a mail"; the cancellation mails are covered by the Go tests.

/** Signs in with the office's demo account from the login page and opens the schedule. */
async function signInAsOffice(page: Page): Promise<void> {
  await gotoDemo(page, '/login');
  const account = page
    .getByRole('region', { name: 'Demo accounts' })
    .getByRole('button', { name: /^Sign in as .+ \(Office\)$/ });
  const label = (await account.getAttribute('aria-label')) ?? '';
  const name = /^Sign in as (.+) \(Office\)$/.exec(label)?.[1] ?? '';
  expect(name).not.toBe('');
  await account.click();
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();

  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
}

/**
 * Opens a public, regular lesson without bookings two to four weeks ahead
 * (it has not started) and returns its card's name and the sheet.
 */
async function openComingLesson(page: Page): Promise<{ name: string; sheet: Locator }> {
  await page.getByRole('button', { name: 'Next 14 days' }).click();
  // The Saturday creative dance class is the seed's one class without
  // members, so its lessons are unbooked on any weekday (members are booked
  // into every lesson of their classes). The 14 days always have a Saturday.
  // A plain card: no status, kind or visibility after the time.
  const card = page
    .getByRole('main')
    .getByRole('button', { name: /^Creative Dance, Saturday, .+, 10:00–10:45$/ })
    .first();
  await expect(card).toBeVisible();
  const name = (await card.getAttribute('aria-label')) ?? '';
  const courseType = name.split(', ')[0] ?? '';
  await card.click();
  const sheet = page.getByRole('dialog', { name: courseType });
  await expect(sheet.getByRole('region', { name: 'Manage this lesson' })).toBeVisible();
  return { name, sheet };
}

/** The schedule card named exactly `name`. */
const cardNamed = (page: Page, name: string) =>
  page.getByRole('main').getByRole('button', { name, exact: true });

/** Today's date at the school (Europe/Berlin), whatever the browser's zone. */
const schoolToday = () =>
  new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Berlin' }).format(new Date());

test('the office cancels a lesson with a note, and it shows as canceled', async ({ page }) => {
  await signInAsOffice(page);
  const { name, sheet } = await openComingLesson(page);

  await sheet.getByRole('button', { name: 'Cancel lesson' }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Cancel this lesson?' });
  await expect(confirm.getByText('Nobody has booked it, so nobody gets a mail.')).toBeVisible();
  await confirm.getByLabel('Note (optional)').fill('The studio floor is being repaired.');
  await confirm.getByRole('button', { name: 'Cancel lesson' }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText('The lesson has been canceled.')).toBeVisible();

  await expect(sheet.getByText('This lesson has been canceled.')).toBeVisible();
  await expect(sheet.getByText(/^Canceled by .+ on .+\.$/)).toBeVisible();
  await expect(sheet.getByText('Note: The studio floor is being repaired.')).toBeVisible();
  await expect(sheet.getByText(/^Edited individually/)).toBeVisible();

  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();
  await expect(cardNamed(page, `${name}, Canceled`)).toBeVisible();

  // And it takes place after all.
  await cardNamed(page, `${name}, Canceled`).click();
  await sheet.getByRole('button', { name: 'Restore' }).click();
  const restore = page.getByRole('alertdialog', { name: 'Restore this lesson?' });
  await restore.getByRole('button', { name: 'Restore lesson' }).click();
  await expect(page.getByText('The lesson takes place again.')).toBeVisible();
  await expect(sheet.getByText('This lesson has been canceled.')).toBeHidden();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(cardNamed(page, name)).toBeVisible();
});

test('the office hides a lesson and shows it again', async ({ page }) => {
  await signInAsOffice(page);
  const { name, sheet } = await openComingLesson(page);

  await sheet.getByRole('button', { name: 'Hide', exact: true }).click();
  await expect(page.getByText('The lesson is hidden: only staff see it.')).toBeVisible();
  await expect(sheet.getByText('Hidden – staff only')).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(cardNamed(page, `${name}, Hidden – staff only`)).toBeVisible();

  await cardNamed(page, `${name}, Hidden – staff only`).click();
  await sheet.getByRole('button', { name: 'Unhide' }).click();
  await expect(page.getByText('The lesson is public again.')).toBeVisible();
  await expect(sheet.getByText('Hidden – staff only')).toBeHidden();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(cardNamed(page, name)).toBeVisible();
});

test('the office edits a lesson in its sheet', async ({ page }) => {
  await signInAsOffice(page);
  const { sheet } = await openComingLesson(page);

  await sheet.getByRole('button', { name: 'Edit' }).click();
  const form = page.getByRole('dialog', { name: 'Edit lesson' });
  await expect(
    form.getByText('Afterwards it no longer follows its class.', { exact: false }),
  ).toBeVisible();
  await form.getByLabel('Capacity').fill('9');
  await form.getByRole('button', { name: 'Save changes' }).click();
  await expect(form).toBeHidden();
  await expect(page.getByText('The lesson has been saved.')).toBeVisible();

  await expect(sheet.getByText('9 places', { exact: true })).toBeVisible();
  await expect(sheet.getByText(/^Edited individually/)).toBeVisible();
});

/** Fills the "New one-off lesson" form for a 45-minute tap workshop in Studio B. */
async function addTapWorkshop(page: Page, date: string, start: string): Promise<Locator> {
  await page.getByRole('button', { name: 'New lesson' }).click();
  const form = page.getByRole('dialog', { name: 'New one-off lesson' });
  await form.getByRole('combobox', { name: 'Course type' }).click();
  await page.getByRole('option', { name: 'Tap' }).click();
  await form.getByLabel('Date').fill(date);
  await form.getByLabel('Start time').fill(start);
  await form.getByLabel('Duration (minutes)').fill('45');
  await form.getByRole('combobox', { name: 'Room' }).click();
  await page.getByRole('option', { name: 'Studio North · Studio B' }).click();
  await expect(form.getByLabel('Capacity')).toHaveValue('14');
  await form.getByRole('combobox', { name: 'Instructor' }).click();
  await page.getByRole('option', { name: 'Nina Tapworth' }).click();
  await form.getByRole('combobox', { name: 'Kind' }).click();
  await page.getByRole('option', { name: 'Workshop' }).click();
  await form.getByRole('button', { name: 'Add lesson' }).click();
  return form;
}

test('the office adds one-off lessons, confirms a taken room and deletes one', async ({ page }) => {
  await signInAsOffice(page);
  const date = addDays(schoolToday(), 20);

  const first = await addTapWorkshop(page, date, '07:00');
  await expect(first).toBeHidden();
  await expect(page.getByText('The lesson has been added.')).toBeVisible();
  // The new lesson opens.
  const sheet = page.getByRole('dialog', { name: 'Tap' });
  await expect(sheet.getByText('Workshop')).toBeVisible();
  await expect(sheet.getByText('07:00–07:45')).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();

  // A second one in the same room overlaps the first.
  const second = await addTapWorkshop(page, date, '07:30');
  const taken = second.getByRole('alert').filter({ hasText: 'Studio B is taken at that time' });
  await expect(taken).toContainText(/Tap, .+, 07:00–07:45 uses the room\./);
  await taken.getByRole('button', { name: 'Save anyway' }).click();
  await expect(second).toBeHidden();
  await expect(sheet.getByText('07:30–08:15')).toBeVisible();

  await sheet.getByRole('button', { name: 'Delete' }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Delete this one-off lesson?' });
  await confirm.getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText('The lesson has been deleted.')).toBeVisible();
  await expect(sheet).toBeHidden();
});

test('the office records a one-off lesson in the past after a confirmation', async ({ page }) => {
  await signInAsOffice(page);

  const form = await addTapWorkshop(page, addDays(schoolToday(), -2), '07:00');
  const past = form.getByRole('alert').filter({ hasText: 'This lesson is in the past' });
  await expect(past).toContainText('nobody gets a mail about a lesson in the past.');
  await past.getByRole('button', { name: 'Save anyway' }).click();
  await expect(form).toBeHidden();
  await expect(page.getByText('The lesson has been added.')).toBeVisible();

  // It has started, so nothing can be changed any more.
  const sheet = page.getByRole('dialog', { name: 'Tap' });
  await expect(
    sheet.getByText('This lesson has started and can no longer be changed.'),
  ).toBeVisible();
  await expect(sheet.getByRole('button', { name: 'Edit' })).toBeDisabled();
});

/** A school date as the schedule's day headings name it, e.g. "Monday, November 16". */
const dayHeading = (date: string) =>
  new Intl.DateTimeFormat('en-US', {
    weekday: 'long',
    month: 'long',
    day: 'numeric',
    timeZone: 'UTC',
  }).format(new Date(`${date}T00:00:00Z`));

test('the office clones a day and the copies show in the schedule', async ({ page }) => {
  await signInAsOffice(page);
  // A one-off lesson at 07:00, when no class runs, is the one lesson of
  // that day the clone copies.
  const source = addDays(schoolToday(), 20);
  const target = addDays(source, 7);
  const added = await addTapWorkshop(page, source, '07:00');
  await expect(added).toBeHidden();
  const lesson = page.getByRole('dialog', { name: 'Tap' });
  await lesson.getByRole('button', { name: 'Close' }).click();
  await expect(lesson).toBeHidden();

  await page.getByRole('button', { name: 'Clone…' }).click();
  const sheet = page.getByRole('dialog', { name: 'Clone lessons' });
  await expect(sheet.getByRole('radio', { name: 'One day' })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  await sheet.getByLabel('Copy from').fill(source);
  await sheet.getByLabel('Copy to').fill(target);
  await sheet.getByRole('button', { name: 'Preview' }).click();

  const preview = sheet.getByRole('region', { name: 'Preview' });
  const planned = preview.getByRole('list', { name: 'Will be created: 1 lesson' });
  const copy = planned.getByRole('listitem');
  await expect(copy.getByText('Tap', { exact: true })).toBeVisible();
  await expect(copy.getByText(`${dayHeading(target)}, 07:00–07:45`)).toBeVisible();
  await expect(copy.getByText(`Copy of ${dayHeading(source)}`)).toBeVisible();
  // The weekly classes of that day (none on a Sunday) are made by their classes in the target week.
  const skipped = preview
    .getByRole('list', { name: /^Skipped: \d+ lessons?$/ })
    .getByRole('listitem');
  await expect(
    skipped.filter({ hasNotText: 'Its class already has this lesson in the target week.' }),
  ).toHaveCount(0);
  await preview.getByRole('button', { name: 'Clone 1 lesson' }).click();

  const result = sheet.getByRole('region', { name: 'Result' });
  await expect(result.getByRole('list', { name: 'Created: 1 lesson' })).toContainText('Tap');
  await expect(page.getByText('1 lesson has been cloned.')).toBeVisible();
  await result.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();

  // Days 14 to 27 from today show both the lesson and its copy.
  await page.getByRole('button', { name: 'Next 14 days' }).click();
  for (const date of [source, target]) {
    await expect(
      page
        .getByRole('region', { name: dayHeading(date) })
        .getByRole('button', { name: `Tap, ${dayHeading(date)}, 07:00–07:45, Workshop` }),
    ).toBeVisible();
  }
});
