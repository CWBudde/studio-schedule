import { expect, type Page, signIn, test } from './fixtures';

// The member import (ADR 0015) of the sample file the page offers: German
// headers, semicolons, two parents per row. The demo seed has Lena
// Quillfeather with the member number M-1042 and Clara and Tom Amberly with
// accounts, whom the file names as parents.

const sample = 'src/features/import/members-sample.csv';

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

test('the office tries the sample file, imports it, and finds the new members with their parents', async ({
  page,
}) => {
  const office = await signIn(page, 'Office');
  await openAdmin(page, office, 'Import', 'Import members');

  // The sample is a download made in the browser.
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download the sample file' }).click();
  expect((await download).suggestedFilename()).toBe('members-sample.csv');

  await page.getByLabel('CSV file').setInputFiles(sample);
  await expect(
    page.getByText('members-sample.csv: 6 rows, UTF-8, separated by semicolons.'),
  ).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Member number' })).toContainText(
    'Mitgliedsnummer',
  );
  await expect(page.getByRole('combobox', { name: 'Parent 2: Email' })).toContainText(
    'E-Mail Elternteil 2',
  );
  await page.getByRole('checkbox', { name: 'Kurs' }).click();

  // The trial changes nothing and tells what would happen.
  await page.getByRole('button', { name: 'Try it' }).click();
  const trial = page.getByRole('table', { name: 'Trial' });
  await expect(trial).toBeVisible();
  const summary = page.getByRole('list', { name: 'Summary' });
  for (const text of ['4 new', '1 updated', '1 not imported', '2 new parents', '5 parent links']) {
    await expect(summary).toContainText(text);
  }
  const paula = trial.getByRole('row').filter({ hasText: 'Paula Amberly' });
  await expect(paula).toContainText('Clara Amberly: found by email, linked as parent');
  await expect(paula).toContainText('Tom Amberly: found by name, linked as parent');
  await expect(trial.getByRole('row').filter({ hasText: 'Ina Fallowmere' })).toContainText(
    'Birth date is not valid.',
  );

  await page.getByRole('button', { name: 'Import 5 rows' }).click();
  await expect(page.getByRole('table', { name: 'Imported' })).toBeVisible();
  const runs = page.getByRole('table', { name: 'Import runs' });
  await expect(runs).toContainText('members-sample.csv');
  await expect(runs).toContainText('4 added, 1 updated, 0 unchanged, 1 not imported');

  // Paula is a person now, with both parents and the course in her notes.
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Persons' })
    .click();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('M-2001');
  await page.getByRole('link', { name: 'Paula Amberly' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Paula Amberly' })).toBeVisible();
  const details = page.getByRole('region', { name: 'Details' });
  await expect(details).toContainText('Kurs: Kreativer Kindertanz');
  await expect(details).toContainText('M-2001');
  const guardians = page.getByRole('region', { name: 'Guardians' });
  await expect(guardians).toContainText('Clara Amberly');
  await expect(guardians).toContainText('Tom Amberly');
});
