import type { Locator } from '@playwright/test';
import { expect, gotoDemo, lessons, type Page, signIn, test } from './fixtures';

// The demo seed has a ballet workshop of Alina Starling (the demo's
// instructor login) 24 days ahead, 13:30–15:30, to which she added a guest
// with an email (Mara Gastfield), a walk-in guest without one (Pia Walkby)
// and Ute Tallowfield on a trial with a note (ADR 0009, amendment). The
// weekly classes of the past week have the bookings of their members.

type Role = 'Instructor' | 'Office';

/**
 * Signs in with the demo account of a role (from the login page, which the
 * header's "Sign in" opens) and opens the schedule; returns the account's name.
 */
async function signInAs(page: Page, role: Role): Promise<string> {
  const name = await signIn(page, role);
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
  return name;
}

/** Opens the sheet of the seed's ballet workshop and returns it with its participants. */
async function openBalletWorkshop(page: Page): Promise<{ sheet: Locator; section: Locator }> {
  await page.getByRole('button', { name: 'Next 14 days' }).click();
  await page
    .getByRole('main')
    .getByRole('button', { name: /^Classical Ballet, .+, 13:30–15:30, Workshop/ })
    .first()
    .click();
  const sheet = page.getByRole('dialog', { name: 'Classical Ballet' });
  return { sheet, section: sheet.getByRole('region', { name: 'Participants' }) };
}

/** The row of a participant in a list of the section. */
const row = (list: Locator, name: string): Locator =>
  list.getByRole('listitem').filter({ hasText: name });

test('the instructor sees her participants, adds a guest and removes them again', async ({
  page,
}) => {
  await gotoDemo(page, '/');
  await signInAs(page, 'Instructor');
  const { sheet, section } = await openBalletWorkshop(page);

  await expect(section.getByText(/^3 of 16 booked · 13 places free$/)).toBeVisible();
  const booked = section.getByRole('list', { name: 'Booked participants' });
  const mara = row(booked, 'Mara Gastfield');
  await expect(mara.getByText('Guest', { exact: true })).toBeVisible();
  await expect(mara.getByRole('link', { name: 'mara.gastfield@example.com' })).toHaveAttribute(
    'href',
    'mailto:mara.gastfield@example.com',
  );
  await expect(row(booked, 'Pia Walkby').getByText('Guest', { exact: true })).toBeVisible();
  const ute = row(booked, 'Ute Tallowfield');
  await expect(ute.getByText('Trial', { exact: true })).toBeVisible();
  await expect(
    ute.getByText('Note: First ballet class in years; mind her left knee.'),
  ).toBeVisible();
  // Her phone, as the seed has it; a tel: link.
  await expect(ute.getByRole('link', { name: /^\+49/ })).toHaveAttribute('href', /^tel:\+49/);
  // Instructors see no booking history, and nothing to take before the start.
  await expect(section.getByRole('button', { name: /^Booking history of/ })).toHaveCount(0);
  await expect(section.getByRole('button', { name: 'All attended' })).toHaveCount(0);

  // The list stacks: nothing is wider than the sheet, on a phone too.
  expect(await sheet.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true);

  // A guest without an email.
  await section.getByRole('button', { name: 'Add participant' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add participant' });
  await dialog.getByRole('tab', { name: 'Guest' }).click();
  await dialog.getByRole('textbox', { name: 'Name' }).fill('Gus Visitor');
  await dialog.getByRole('button', { name: 'Add' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('Gus Visitor has been added.')).toBeVisible();
  await expect(section.getByText(/^4 of 16 booked · 12 places free$/)).toBeVisible();
  await expect(row(booked, 'Gus Visitor').getByText('Guest', { exact: true })).toBeVisible();

  // … and off the list again, after a confirmation.
  await section.getByRole('button', { name: 'Remove Gus Visitor' }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Remove Gus Visitor?' });
  await confirm.getByRole('button', { name: 'Remove' }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText('Gus Visitor has been removed.')).toBeVisible();
  await expect(section.getByText(/^3 of 16 booked · 13 places free$/)).toBeVisible();
  await expect(row(booked, 'Gus Visitor')).toHaveCount(0);
  const canceled = section.getByRole('list', { name: 'Canceled bookings' });
  await expect(row(canceled, 'Gus Visitor').getByText('Canceled', { exact: true })).toBeVisible();
});

test('the office sees the history of a booking', async ({ page }) => {
  await gotoDemo(page, '/');
  await signInAs(page, 'Office');
  const { section } = await openBalletWorkshop(page);

  await section.getByRole('button', { name: 'Booking history of Ute Tallowfield' }).click();
  const dialog = page.getByRole('dialog', { name: 'Booking history' });
  const events = dialog.getByRole('list', { name: 'Booking history' });
  await expect(events.getByRole('listitem')).toHaveCount(1);
  await expect(events.getByText('Booked')).toBeVisible();
  await expect(events.getByText(/^by Alina Starling \(.+@.+\)$/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Close' }).click();
  await expect(dialog).toBeHidden();
});

test('the office takes attendance of a past lesson', async ({ page }) => {
  await gotoDemo(page, '/');
  await signInAs(page, 'Office');
  await page.getByRole('button', { name: 'Previous 14 days' }).click();
  await expect(lessons(page.getByRole('main')).first()).toBeVisible();

  // Every ballet class of the seed has members, booked into last week's lessons.
  await page
    .getByRole('main')
    .getByRole('button', { name: /^Classical Ballet, / })
    .first()
    .click();
  const sheet = page.getByRole('dialog', { name: 'Classical Ballet' });
  const section = sheet.getByRole('region', { name: 'Participants' });
  const attended = section
    .getByRole('list', { name: 'Booked participants' })
    .getByRole('button', { name: / attended$/ })
    .first();
  const label = (await attended.getAttribute('aria-label')) ?? '';
  const name = label.replace(/ attended$/, '');
  expect(name).not.toBe('');
  await expect(attended).toHaveAttribute('aria-pressed', 'false');

  await attended.click();
  await expect(page.getByText(`${name} attended.`)).toBeVisible();
  await expect(section.getByRole('button', { name: `${name} attended` })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(
    section.getByRole('button', { name: `Reset the attendance of ${name}` }),
  ).toBeVisible();
});
