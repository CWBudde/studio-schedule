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

/** Opens a section of the administration from its navigation. */
async function openSection(page: Page, section: string, heading = section): Promise<void> {
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: section })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
}

/** The options of the schedule's instructor filter, read from its open list. */
async function instructorOptions(page: Page): Promise<string[]> {
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
  await page.getByRole('combobox', { name: 'Instructor' }).click();
  const options = page.getByRole('option');
  await expect(options.first()).toBeVisible();
  const names = await options.allTextContents();
  await page.keyboard.press('Escape');
  await expect(options).toHaveCount(0);
  return names;
}

test('the office adds, renames and deletes a course type', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  await openAdministration(page, office);
  await openSection(page, 'Course types');

  await page.getByRole('button', { name: 'Add course type' }).click();
  const add = page.getByRole('dialog', { name: 'Add a course type' });
  await add.getByLabel('Name').fill('Swing Dance');
  await add.getByLabel('Description (optional)').fill('Partner dancing to big band music.');
  await add.getByRole('button', { name: 'Add course type' }).click();
  await expect(add).toBeHidden();
  await expect(page.getByText('Swing Dance has been added.')).toBeVisible();

  // Names are unique ignoring case.
  await page.getByRole('button', { name: 'Edit Swing Dance' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit Swing Dance' });
  await expect(edit.getByLabel('Description (optional)')).toHaveValue(
    'Partner dancing to big band music.',
  );
  await edit.getByLabel('Name').fill('tap');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit.getByRole('alert')).toHaveText('Another course type is already called “tap”.');
  await edit.getByLabel('Name').fill('Lindy Hop');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit).toBeHidden();
  await expect(page.getByText('Lindy Hop has been saved.')).toBeVisible();

  await page.getByRole('button', { name: 'Delete Lindy Hop' }).click();
  await page
    .getByRole('alertdialog', { name: 'Delete Lindy Hop?' })
    .getByRole('button', { name: 'Delete' })
    .click();
  await expect(page.getByText('Lindy Hop has been deleted.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit Lindy Hop' })).toHaveCount(0);
});

test('deleting a course type that classes use says why it stays', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  await openAdministration(page, office);
  await openSection(page, 'Course types');

  await page.getByRole('button', { name: 'Delete Classical Ballet' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Delete Classical Ballet?' });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(dialog.getByRole('alert')).toHaveText(
    'Classical Ballet is used by classes or lessons, so it cannot be deleted.',
  );
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(page.getByRole('button', { name: 'Edit Classical Ballet' })).toBeVisible();
});

test('a deactivated instructor leaves the schedule filter', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  expect(await instructorOptions(page)).toContain('Nina Tapworth');

  await openAdministration(page, office);
  await openSection(page, 'Staff');

  // Staff who teach cannot be deleted; the dialog deactivates them instead.
  await page.getByRole('button', { name: 'Delete Nina Tapworth' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Delete Nina Tapworth?' });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Deactivate them instead');
  await dialog.getByRole('button', { name: 'Deactivate instead' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('Nina Tapworth has been deactivated.')).toBeVisible();

  // Only the active flag changed.
  await page.getByRole('button', { name: 'Edit Nina Tapworth' }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit Nina Tapworth' });
  await expect(sheet.getByRole('combobox', { name: 'Role' })).toHaveText('Instructor');
  await expect(sheet.getByRole('switch', { name: 'Active' })).not.toBeChecked();
  await sheet.getByRole('button', { name: 'Cancel' }).click();
  await expect(sheet).toBeHidden();

  // The office still lists former staff.
  await page.getByRole('radio', { name: 'Inactive' }).click();
  await expect(page.getByRole('button', { name: 'Edit Nina Tapworth' })).toBeVisible();

  const options = await instructorOptions(page);
  expect(options).not.toContain('Nina Tapworth');
  expect(options).toContain('Alina Starling');
});

test('the office links a staff member to a person', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  await openAdministration(page, office);
  await openSection(page, 'Staff');

  await page.getByRole('button', { name: 'Edit Alina Starling' }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit Alina Starling' });
  const person = sheet.getByRole('combobox', { name: 'Person (optional)' });
  await expect(person).toHaveText('Alina Starling');

  await sheet.getByRole('button', { name: 'Remove the person' }).click();
  await expect(person).toHaveText('No person');
  await person.click();
  await page.getByRole('combobox', { name: 'Search by name or email' }).fill('starling');
  await page.getByRole('option', { name: /Alina Starling/ }).click();
  await expect(person).toHaveText('Alina Starling');
  await sheet.getByRole('button', { name: 'Save changes' }).click();
  await expect(sheet).toBeHidden();
  await expect(page.getByText('Alina Starling has been saved.')).toBeVisible();
});

/** Opens the weekly classes of the demo's term from the administration. */
async function openTermClasses(page: Page, office: string): Promise<void> {
  await openAdministration(page, office);
  await openSection(page, 'Timetable', 'Terms');
  await page.getByRole('link', { name: /^Classes of School Year / }).click();
  await expect(
    page.getByRole('heading', { level: 1, name: /^Classes – School Year / }),
  ).toBeVisible();
}

/** Opens a page of the main navigation, e.g. the public classes. */
async function openMain(page: Page, link: string): Promise<void> {
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: link }).click();
  await expect(page.getByRole('heading', { level: 1, name: link })).toBeVisible();
}

/** A class card, named "<course type>, <weekday>, <hh:mm–hh:mm>". */
const classCard = (page: Page, name: string) =>
  page.getByRole('main').getByRole('article', { name, exact: true });

test('the office adds a class, moves it, and the public classes follow', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  await openTermClasses(page, office);

  await page.getByRole('button', { name: 'Add class' }).click();
  const add = page.getByRole('dialog', { name: 'Add a class' });
  await add.getByRole('combobox', { name: 'Course type' }).click();
  await page.getByRole('option', { name: 'Tap', exact: true }).click();
  await add.getByRole('combobox', { name: 'Room' }).click();
  await page.getByRole('option', { name: 'Studio South · Hall 2' }).click();
  // The capacity of the room.
  await expect(add.getByLabel('Capacity')).toHaveValue('12');
  await add.getByRole('combobox', { name: 'Instructor' }).click();
  await page.getByRole('option', { name: 'Nina Tapworth' }).click();
  await add.getByRole('combobox', { name: 'Level' }).click();
  await page.getByRole('option', { name: 'Beginner' }).click();
  await add.getByRole('combobox', { name: 'Weekday' }).click();
  await page.getByRole('option', { name: 'Sunday' }).click();
  await add.getByLabel('Start time').fill('09:15');
  await add.getByLabel('Duration (minutes)').fill('60');
  await add.getByRole('button', { name: 'Add class' }).click();
  await expect(add).toBeHidden();
  await expect(page.getByText('Tap, Sunday 09:15 has been added.')).toBeVisible();
  await expect(classCard(page, 'Tap, Sunday, 09:15–10:15')).toBeVisible();

  await page.getByRole('button', { name: 'Edit Tap, Sunday 09:15' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit Tap, Sunday 09:15' });
  await expect(edit.getByLabel('Start time')).toHaveValue('09:15');
  await edit.getByLabel('Start time').fill('10:45');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit).toBeHidden();
  await expect(page.getByText('Tap, Sunday 10:45 has been saved.')).toBeVisible();

  await openMain(page, 'Classes');
  await expect(classCard(page, 'Tap, Sunday, 10:45–11:45')).toBeVisible();
  await expect(classCard(page, 'Tap, Sunday, 09:15–10:15')).toHaveCount(0);
});

test('office edits a class and the schedule shows the change', async ({ page }) => {
  const office = await signInAs(page, 'Office');
  await openTermClasses(page, office);

  await page.getByRole('button', { name: 'Edit Barre Fitness, Tuesday 19:00' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit Barre Fitness, Tuesday 19:00' });
  await edit.getByLabel('Start time').fill('07:15');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit).toBeHidden();
  await expect(page.getByText('Barre Fitness, Tuesday 07:15 has been saved.')).toBeVisible();

  // The next 14 days hold a Tuesday whose lesson moved along.
  await openMain(page, 'Schedule');
  await expect(
    page
      .getByRole('main')
      .getByRole('button', { name: /^Barre Fitness, .+, 07:15–08:15/ })
      .first(),
  ).toBeVisible();
});

test('a class with history is retired instead of deleted and leaves the public classes', async ({
  page,
}) => {
  const office = await signInAs(page, 'Office');
  await openMain(page, 'Classes');
  await expect(classCard(page, 'Pointe Work, Friday, 18:00–19:15')).toBeVisible();

  await openTermClasses(page, office);
  await page.getByRole('button', { name: 'Delete Pointe Work, Friday 18:00' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Delete Pointe Work, Friday 18:00?' });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Retire it instead.');
  await dialog.getByRole('button', { name: 'Retire instead' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('Pointe Work, Friday 18:00 has been retired.')).toBeVisible();
  const retired = classCard(page, 'Pointe Work, Friday, 18:00–19:15');
  await expect(retired.getByText('Retired')).toBeVisible();
  await expect(
    retired.getByRole('button', { name: 'Reactivate Pointe Work, Friday 18:00' }),
  ).toBeVisible();

  await openMain(page, 'Classes');
  await expect(classCard(page, 'Pointe Work, Wednesday, 18:00–18:45')).toBeVisible();
  await expect(classCard(page, 'Pointe Work, Friday, 18:00–19:15')).toHaveCount(0);
});
