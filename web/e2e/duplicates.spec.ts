import { expect, type Page, signIn, signOut, test } from './fixtures';

// The demo seed has two pairs of duplicates (ADR 0015): Lena Quillfeather
// twice with the same email (a likely pair; one has the member number
// M-1042, the other a birth date and a note), and Jonas Brightwater twice
// with the same name (a possible pair).

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

const lenaPair = 'Lena Quillfeather / Lena Quillfeather';
const jonasPair = 'Jonas Brightwater / Jonas Brightwater';

test('the office merges a duplicate pair after its preview and dismisses another', async ({
  page,
}) => {
  const office = await signIn(page, 'Office');
  await openAdmin(page, office, 'Duplicates');

  const list = page.getByRole('list', { name: 'Possible duplicates' });
  await expect(list.getByRole('listitem', { name: lenaPair })).toContainText('Likely');
  await expect(list.getByRole('listitem', { name: jonasPair })).toContainText('Possible');

  await page.getByRole('button', { name: `Merge ${lenaPair}` }).click();
  const dialog = page.getByRole('dialog', { name: 'Merge two persons' });
  // The first stays; its empty birth date takes the duplicate's.
  await expect(dialog.getByRole('radio', { name: 'Keep Lena Quillfeather' }).first()).toBeChecked();
  await expect(dialog.getByRole('radio', { name: 'August 17, 1994' })).toBeChecked();
  // The notes of both, which the office may edit.
  await expect(dialog.getByRole('textbox', { name: 'Notes of the person that stays' })).toHaveValue(
    'Registered at the front desk.',
  );
  const preview = dialog.getByRole('region', { name: 'What happens' });
  await expect(preview).toContainText('Lena Quillfeather stays, with:');
  await expect(preview).toContainText('M-1042');
  await expect(preview).toContainText('August 17, 1994');
  await dialog.getByRole('button', { name: 'Merge', exact: true }).click();
  await expect(
    page.getByText('Lena Quillfeather was merged into Lena Quillfeather.'),
  ).toBeVisible();
  await expect(list.getByRole('listitem', { name: lenaPair })).toHaveCount(0);

  // One Lena is left, with the fields of both.
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Persons' })
    .click();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('Quillfeather');
  await expect(page.getByRole('link', { name: 'Lena Quillfeather' })).toHaveCount(1);
  await page.getByRole('link', { name: 'Lena Quillfeather' }).click();
  const details = page.getByRole('region', { name: 'Details' });
  await expect(details).toContainText('M-1042');
  await expect(details).toContainText('August 17, 1994');
  await expect(details).toContainText('Registered at the front desk.');
  await expect(details.getByRole('link', { name: 'Lena Quillfeather' })).toBeVisible();

  // The other pair is not the same.
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Duplicates' })
    .click();
  await page.getByRole('button', { name: `${jonasPair} are not the same` }).click();
  await expect(page.getByText('No possible duplicates.')).toBeVisible();
  await signOut(page, office);

  // The merge is in the audit log.
  const admin = await signIn(page, 'Administrator');
  await openAdmin(page, admin, 'Audit log');
  // A table on wide screens, a list on narrow ones.
  const log = page
    .getByRole('table', { name: 'Audit log' })
    .or(page.getByRole('list', { name: 'Audit log' }));
  await expect(log).toContainText('Duplicate merged');
  await expect(log).toContainText('merged: Lena Quillfeather');
});
