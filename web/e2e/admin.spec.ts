import { expect, gotoDemo, type Page, test } from './fixtures';

/** Signs in with the demo account of a role from the login page; returns its name. */
async function signInAs(page: Page, role: string): Promise<string> {
  await gotoDemo(page, '/login');
  const button = page
    .getByRole('region', { name: 'Demo accounts' })
    .getByRole('button', { name: new RegExp(`^Sign in as .+ \\(${role}\\)$`) });
  const label = (await button.getAttribute('aria-label')) ?? '';
  const name = new RegExp(`^Sign in as (.+) \\(${role}\\)$`).exec(label)?.[1] ?? '';
  expect(name).not.toBe('');
  await button.click();
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
  return name;
}

/** Opens the administration from the account menu; it starts with the locations. */
async function openAdministration(page: Page, name: string): Promise<void> {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Locations and rooms' })).toBeVisible();
}

/** The rooms: a table on wider screens, a list of cards on phones. */
const roomsList = (page: Page) =>
  page.getByRole('table', { name: 'Rooms' }).or(page.getByRole('list', { name: 'Rooms' }));

test('the office adds, renames and deletes a location and a room', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  await openAdministration(page, office);
  await expect(
    page
      .getByRole('navigation', { name: 'Administration' })
      .getByRole('link', { name: 'Accounts' }),
  ).toHaveCount(0);

  await page.getByRole('button', { name: 'Add location' }).click();
  const addLocation = page.getByRole('dialog', { name: 'Add a location' });
  await addLocation.getByLabel('Name').fill('Lantern Hall');
  await addLocation.getByLabel('Address (optional)').fill('7 Example Lane, Sampletown');
  await addLocation.getByRole('button', { name: 'Add location' }).click();
  await expect(addLocation).toBeHidden();
  await expect(page.getByText('Lantern Hall has been added.')).toBeVisible();

  await page.getByRole('button', { name: 'Add room' }).click();
  const addRoom = page.getByRole('dialog', { name: 'Add a room' });
  await addRoom.getByLabel('Name').fill('Loft');
  await addRoom.getByRole('combobox', { name: 'Location' }).click();
  await page.getByRole('option', { name: 'Lantern Hall' }).click();
  await addRoom.getByLabel('Capacity').fill('14');
  await addRoom.getByRole('button', { name: 'Add room' }).click();
  await expect(addRoom).toBeHidden();
  await expect(page.getByText('Loft has been added.')).toBeVisible();

  await page.getByRole('button', { name: 'Edit Loft (Lantern Hall)' }).click();
  const editRoom = page.getByRole('dialog', { name: 'Edit Loft' });
  await expect(editRoom.getByLabel('Capacity')).toHaveValue('14');
  await editRoom.getByLabel('Name').fill('Upper Loft');
  await editRoom.getByRole('button', { name: 'Save changes' }).click();
  await expect(editRoom).toBeHidden();
  await expect(page.getByText('Upper Loft has been saved.')).toBeVisible();

  await page.getByRole('button', { name: 'Edit Lantern Hall' }).click();
  const editLocation = page.getByRole('dialog', { name: 'Edit Lantern Hall' });
  await editLocation.getByLabel('Name').fill('Lantern House');
  await editLocation.getByRole('button', { name: 'Save changes' }).click();
  await expect(editLocation).toBeHidden();
  await expect(page.getByText('Lantern House has been saved.')).toBeVisible();
  const room = page.getByRole('button', { name: 'Delete Upper Loft (Lantern House)' });
  await expect(room).toBeVisible();

  // A location with rooms stays.
  await page.getByRole('button', { name: 'Delete Lantern House' }).click();
  const deleteLocation = page.getByRole('alertdialog', { name: 'Delete Lantern House?' });
  await deleteLocation.getByRole('button', { name: 'Delete' }).click();
  await expect(deleteLocation.getByRole('alert')).toHaveText(
    'Lantern House still has rooms. Delete its rooms first.',
  );
  await deleteLocation.getByRole('button', { name: 'Cancel' }).click();
  await expect(deleteLocation).toBeHidden();

  await room.click();
  const deleteRoom = page.getByRole('alertdialog', { name: 'Delete Upper Loft?' });
  await deleteRoom.getByRole('button', { name: 'Delete' }).click();
  await expect(deleteRoom).toBeHidden();
  await expect(page.getByText('Upper Loft has been deleted.')).toBeVisible();
  await expect(room).toHaveCount(0);

  await page.getByRole('button', { name: 'Delete Lantern House' }).click();
  await page
    .getByRole('alertdialog', { name: 'Delete Lantern House?' })
    .getByRole('button', { name: 'Delete' })
    .click();
  await expect(page.getByText('Lantern House has been deleted.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit Lantern House' })).toHaveCount(0);
});

test('deleting a room that classes use says why it stays', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  await openAdministration(page, office);

  const deleteButton = roomsList(page)
    .getByRole('button', { name: /^Delete .+ \(.+\)$/ })
    .first();
  const label = (await deleteButton.getAttribute('aria-label')) ?? '';
  const roomName = /^Delete (.+) \(.+\)$/.exec(label)?.[1] ?? '';
  expect(roomName).not.toBe('');

  await deleteButton.click();
  const dialog = page.getByRole('alertdialog', { name: `Delete ${roomName}?` });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(dialog.getByRole('alert')).toHaveText(
    `${roomName} is used by classes or lessons, so it cannot be deleted.`,
  );
  await expect(dialog.getByRole('button', { name: 'Delete' })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(roomsList(page).getByRole('button', { name: label })).toBeVisible();
});

test('a member has no access to the administration', async ({ page }) => {
  const member = await signInAs(page, 'Member');
  await page.getByRole('banner').getByRole('button', { name: member }).click();
  await expect(page.getByRole('menuitem', { name: 'Sign out' })).toBeVisible();
  await expect(page.getByRole('menuitem', { name: 'Administration' })).toHaveCount(0);
  await page.keyboard.press('Escape');

  await gotoDemo(page, '/admin/locations');
  await expect(page.getByRole('heading', { level: 1, name: 'No access' })).toBeVisible();
  await expect(
    page.getByText('Only the office and administrators can see this page.'),
  ).toBeVisible();
});
