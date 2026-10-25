import { readFile } from 'node:fs/promises';
import type { Locator } from '@playwright/test';
import { demoEmail, expect, gotoDemo, type Page, signIn, test } from './fixtures';

// Privacy and account administration (ADR 0016) in the demo: the data
// export (a file made in the browser), anonymizing a person, the admin's
// account actions and deleting one's own account.

/** Opens a section of the administration from the account menu. */
async function openAdmin(page: Page, name: string, section: string, heading = section) {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: section })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
}

/** Opens the account settings from the account menu in the header. */
async function openAccount(page: Page, name: string): Promise<void> {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Account settings' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
}

/** Clicks `button` and returns the name and the parsed JSON of the file it saves. */
async function downloadJson(page: Page, button: Locator) {
  const download = page.waitForEvent('download');
  await button.click();
  const file = await download;
  const json = JSON.parse(await readFile(await file.path(), 'utf8')) as {
    format: string;
    version: number;
    person: { givenName: string; familyName: string };
    account?: { email: string };
    bookings: unknown[];
  };
  return { filename: file.suggestedFilename(), json };
}

/** Opens a person of the office's persons view by its search and name. */
async function openPerson(page: Page, query: string, name: string) {
  await page.getByRole('searchbox', { name: 'Search persons' }).fill(query);
  await page.getByRole('link', { name }).click();
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
}

test('a member downloads her own data and her child’s', async ({ page }) => {
  await gotoDemo(page, '/login');
  const email = await demoEmail(page, 'Member');
  const name = await signIn(page, 'Member');
  await openAccount(page, name);

  const card = page.getByRole('region', { name: 'Your data' });
  await expect(card.getByText(`${name} (you)`)).toBeVisible();

  const own = await downloadJson(
    page,
    card.getByRole('button', { name: `Download the data of ${name}` }),
  );
  expect(own.filename).toMatch(/^data-export-[a-z-]+-\d{4}-\d{2}-\d{2}\.json$/);
  expect(own.json.format).toBe('studio-schedule.person-export');
  expect(own.json.version).toBe(1);
  expect(`${own.json.person.givenName} ${own.json.person.familyName}`).toBe(name);
  expect(own.json.account?.email).toBe(email);

  // Each child has a download of its own, without the parent's account.
  const children = card.getByRole('button', { name: /^Download the data of / });
  await expect(children).not.toHaveCount(1);
  const child = await downloadJson(page, children.nth(1));
  expect(child.json.account).toBeUndefined();
});

test('the office exports and anonymizes a person', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openAdmin(page, office, 'Persons');

  // The only guardian of two children is refused.
  await openPerson(page, 'hanna birchmoor', 'Hanna Birchmoor');
  await page.getByRole('button', { name: 'Anonymize Hanna Birchmoor' }).click();
  let dialog = page.getByRole('alertdialog', { name: 'Anonymize Hanna Birchmoor?' });
  await dialog.getByRole('button', { name: 'Anonymize' }).click();
  await expect(dialog.getByText(/is the only guardian of a child/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();

  await page.getByRole('link', { name: 'All persons' }).click();
  await openPerson(page, 'kai lanternby', 'Kai Lanternby');
  const exported = await downloadJson(
    page,
    page.getByRole('button', { name: 'Export the data of Kai Lanternby' }),
  );
  expect(exported.json.person.familyName).toBe('Lanternby');
  expect(exported.json.account?.email).toBe('kai.lanternby@example.org');

  await page.getByRole('button', { name: 'Anonymize Kai Lanternby' }).click();
  dialog = page.getByRole('alertdialog', { name: 'Anonymize Kai Lanternby?' });
  await expect(dialog.getByText(/Past bookings, passes and receipts stay/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Anonymize' }).click();
  await expect(page.getByText('Kai Lanternby has been anonymized.')).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: 'Deleted person' })).toBeVisible();
  await expect(page.getByText(/This person’s data was erased on/)).toBeVisible();

  // The search no longer finds him.
  await page.getByRole('link', { name: 'All persons' }).click();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('lanternby');
  await expect(page.getByText('Nobody matches “lanternby”.')).toBeVisible();
});

test('the admin disables, enables, re-roles and deletes an account', async ({ page }) => {
  const admin = await signIn(page, 'Administrator');
  await openAdmin(page, admin, 'Accounts');
  const table = page.getByRole('table', { name: 'Accounts' });
  const row = table.getByRole('row').filter({ hasText: 'Oskar Nettlebury' });

  // Not the admin's own account.
  await expect(table.getByRole('button', { name: `More actions for ${admin}` })).toBeDisabled();

  const menu = () =>
    table.getByRole('button', { name: 'More actions for Oskar Nettlebury' }).click();

  await menu();
  await page.getByRole('menuitem', { name: 'Disable' }).click();
  let dialog = page.getByRole('alertdialog', { name: 'Disable the account of Oskar Nettlebury?' });
  await dialog.getByRole('button', { name: 'Disable' }).click();
  await expect(page.getByText('Oskar Nettlebury can no longer sign in.')).toBeVisible();
  await expect(row.getByText('Disabled')).toBeVisible();

  await menu();
  await page.getByRole('menuitem', { name: 'Enable' }).click();
  await expect(page.getByText('Oskar Nettlebury can sign in again.')).toBeVisible();
  await expect(row.getByText('Active')).toBeVisible();

  await menu();
  await page.getByRole('menuitem', { name: 'Change role' }).click();
  dialog = page.getByRole('alertdialog', { name: 'Change the role of Oskar Nettlebury' });
  await dialog.getByRole('combobox', { name: 'Role' }).click();
  await page.getByRole('option', { name: 'Instructor' }).click();
  await dialog.getByRole('button', { name: 'Save role' }).click();
  await expect(page.getByText('Oskar Nettlebury is now: Instructor.')).toBeVisible();
  await expect(row.getByRole('cell', { name: 'Instructor' })).toBeVisible();

  // The office issued the demo's receipts: its account is kept.
  await table.getByRole('button', { name: /^More actions for Olga/ }).click();
  await page.getByRole('menuitem', { name: 'Delete' }).click();
  dialog = page.getByRole('alertdialog', { name: /^Delete the account of Olga/ });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(dialog.getByText(/so it cannot be deleted\. Disable it instead\./)).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel' }).click();

  await menu();
  await page.getByRole('menuitem', { name: 'Delete' }).click();
  dialog = page.getByRole('alertdialog', { name: 'Delete the account of Oskar Nettlebury?' });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText('The account of Oskar Nettlebury has been deleted.')).toBeVisible();
  await expect(row).toHaveCount(0);

  // The audit log names what happened.
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Audit log' })
    .click();
  // A table on wider screens, a list of cards on phones.
  const log = page
    .getByRole('table', { name: 'Audit log' })
    .or(page.getByRole('list', { name: 'Audit log' }));
  for (const action of ['Account deleted', 'Role changed', 'Account enabled', 'Account disabled']) {
    await expect(log.getByText(action, { exact: true }).first()).toBeVisible();
  }
});

test('a member deletes her own account and is signed out', async ({ page }) => {
  await gotoDemo(page, '/login');
  const email = await demoEmail(page, 'Member');
  const name = await signIn(page, 'Member');
  await openAccount(page, name);

  const card = page.getByRole('region', { name: 'Delete account' });
  await card.getByLabel('Current password').fill('wrong-password');
  await card.getByRole('button', { name: 'Delete my account' }).click();
  let dialog = page.getByRole('alertdialog', { name: 'Delete your account?' });
  await dialog.getByRole('button', { name: 'Delete account' }).click();
  await expect(card.getByText('The password is incorrect.')).toBeVisible();

  await card.getByLabel('Current password').fill('demo-password');
  await card.getByRole('button', { name: 'Delete my account' }).click();
  dialog = page.getByRole('alertdialog', { name: 'Delete your account?' });
  await dialog.getByRole('button', { name: 'Delete account' }).click();
  await expect(page.getByText('Your account has been deleted.')).toBeVisible();
  await expect(page.getByRole('banner').getByRole('link', { name: 'Sign in' })).toBeVisible();

  // Signing in with the old address fails.
  await page.getByRole('banner').getByRole('link', { name: 'Sign in' }).click();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('demo-password');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByText('The email address or password is incorrect.')).toBeVisible();
});
