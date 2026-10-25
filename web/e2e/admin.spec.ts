import { demoName, expect, gotoDemo, type Page, signIn, test } from './fixtures';

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
  const office = await signIn(page, 'Office');
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
  const office = await signIn(page, 'Office');
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
  const member = await signIn(page, 'Member');
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
  const office = await signIn(page, 'Office');
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
  const office = await signIn(page, 'Office');
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
  const office = await signIn(page, 'Office');
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
  const office = await signIn(page, 'Office');
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
  const office = await signIn(page, 'Office');
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
  const office = await signIn(page, 'Office');
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
  const office = await signIn(page, 'Office');
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

/** Opens the terms from the administration. */
async function openTerms(page: Page, office: string): Promise<void> {
  await openAdministration(page, office);
  await openSection(page, 'Timetable', 'Terms');
}

test('the office adds a term, renames and shortens it, and deletes it', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openTerms(page, office);

  await page.getByRole('button', { name: 'Add term' }).click();
  const add = page.getByRole('dialog', { name: 'Add a term' });
  await add.getByLabel('Name').fill('Summer Intensive');
  await add.getByLabel('First day').fill('2031-07-01');
  await add.getByLabel('Last day').fill('2031-08-29');
  await add.getByRole('button', { name: 'Add term' }).click();
  await expect(add).toBeHidden();
  await expect(page.getByText('Summer Intensive has been added.')).toBeVisible();
  await expect(page.getByText('Jul 1 – Aug 29, 2031')).toBeVisible();

  await page.getByRole('button', { name: 'Edit Summer Intensive' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit Summer Intensive' });
  await expect(edit.getByLabel('First day')).toHaveValue('2031-07-01');
  await expect(edit.getByLabel('Last day')).toHaveValue('2031-08-29');
  // The last day cannot come before the first.
  await edit.getByLabel('Last day').fill('2031-06-30');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit.getByText('The last day cannot be before the first day.')).toBeVisible();
  await edit.getByLabel('Name').fill('Summer Workshop Weeks');
  await edit.getByLabel('Last day').fill('2031-08-15');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit).toBeHidden();
  await expect(page.getByText('Summer Workshop Weeks has been saved.')).toBeVisible();
  await expect(page.getByText('Jul 1 – Aug 15, 2031')).toBeVisible();

  await page.getByRole('button', { name: 'Delete Summer Workshop Weeks' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Delete Summer Workshop Weeks?' });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByText('Summer Workshop Weeks has been deleted.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit Summer Workshop Weeks' })).toHaveCount(0);
});

test('deleting a term with classes says why it stays', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openTerms(page, office);

  const deleteButton = page.getByRole('button', { name: /^Delete School Year / });
  const label = (await deleteButton.getAttribute('aria-label')) ?? '';
  const term = /^Delete (.+)$/.exec(label)?.[1] ?? '';
  expect(term).not.toBe('');

  await deleteButton.click();
  const dialog = page.getByRole('alertdialog', { name: `Delete ${term}?` });
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(dialog.getByRole('alert')).toHaveText(
    `${term} still has classes, so it cannot be deleted. Delete its classes first, or let the term end on its last day.`,
  );
  await dialog.getByRole('button', { name: 'Cancel' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('button', { name: `Edit ${term}` })).toBeVisible();
});

test('a term cannot leave out lessons that took place', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openTerms(page, office);

  await page.getByRole('button', { name: /^Edit School Year / }).click();
  const edit = page.getByRole('dialog', { name: /^Edit School Year / });
  // Starting on its last day would leave out the lessons of the past weeks.
  const lastDay = await edit.getByLabel('Last day').inputValue();
  await edit.getByLabel('First day').fill(lastDay);
  await edit.getByRole('button', { name: 'Save changes' }).click();

  await expect(edit.getByRole('alert')).toHaveText('Please check your entries.');
  await expect(edit.getByLabel('First day')).toHaveAttribute('aria-invalid', 'true');
  await expect(edit.getByLabel('Last day')).not.toHaveAttribute('aria-invalid', 'true');
  await expect(
    edit.getByText(
      'A lesson outside these dates has started, has bookings or was changed one by one, so the term cannot leave it out.',
    ),
  ).toBeVisible();
});

test('the office adds and removes a class member', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openTermClasses(page, office);

  await page.getByRole('button', { name: 'Members of Creative Dance, Saturday 10:00' }).click();
  const sheet = page.getByRole('dialog', { name: 'Members of Creative Dance, Saturday 10:00' });
  await expect(sheet.getByText('This class has no members yet.')).toBeVisible();

  /** Picks a person by a search in the sheet's person field. */
  const pick = async (search: string, name: RegExp) => {
    await sheet.getByRole('combobox', { name: 'Person' }).click();
    await page.getByRole('combobox', { name: 'Search by name or email' }).fill(search);
    await page.getByRole('option', { name }).click();
    await expect(sheet.getByRole('combobox', { name: 'Person' })).not.toHaveText('No person');
  };

  await pick('inkwell', /Irene Inkwell/);
  await sheet.getByLabel('Member since (optional)').fill('2026-09-01');
  await sheet.getByRole('button', { name: 'Add member' }).click();
  await expect(page.getByText('Irene Inkwell has been added to the class.')).toBeVisible();
  const members = sheet.getByRole('list', { name: 'Members' });
  await expect(members.getByRole('listitem')).toHaveCount(1);
  await expect(members.getByText('Member since Sep 1, 2026')).toBeVisible();
  await expect(sheet.getByRole('combobox', { name: 'Person' })).toHaveText('No person');

  // Adding the same person twice says why.
  await pick('inkwell', /Irene Inkwell/);
  await sheet.getByRole('button', { name: 'Add member' }).click();
  await expect(sheet.getByRole('alert')).toHaveText(
    'Irene Inkwell is a member of this class already.',
  );
  await expect(members.getByRole('listitem')).toHaveCount(1);

  await sheet.getByRole('button', { name: 'Remove Irene Inkwell' }).click();
  const question = sheet.getByRole('group', {
    name: 'Remove Irene Inkwell from Creative Dance, Saturday 10:00?',
  });
  await question.getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByText('Irene Inkwell has been removed from the class.')).toBeVisible();
  await expect(sheet.getByText('This class has no members yet.')).toBeVisible();
});

test('the office edits a person, links a guardian and creates an account with an invitation', async ({
  page,
}) => {
  const office = await signIn(page, 'Office');
  await openAdministration(page, office);
  await openSection(page, 'Persons');

  const search = page.getByRole('searchbox', { name: 'Search persons' });
  await search.fill('amberly ella');
  await expect(page.getByRole('link', { name: /Amberly/ })).toHaveCount(1);
  await page.getByRole('link', { name: 'Ella Amberly' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Ella Amberly' })).toBeVisible();
  // Going back keeps the search, which never shows in the address.
  await page.goBack();
  await expect(search).toHaveValue('amberly ella');
  expect(page.url()).not.toContain('amberly');
  await page.getByRole('link', { name: 'Ella Amberly' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Ella Amberly' })).toBeVisible();

  await page.getByRole('button', { name: 'Edit the details of Ella Amberly' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit Ella Amberly' });
  await edit.getByLabel('Phone (optional)').fill('+49 000 0000777');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit).toBeHidden();
  await expect(page.getByText('Ella Amberly has been saved.')).toBeVisible();
  await expect(
    page.getByRole('region', { name: 'Details' }).getByText('+49 000 0000777'),
  ).toBeVisible();

  const guardians = page.getByRole('region', { name: 'Guardians' });
  await expect(guardians.getByRole('link', { name: 'Clara Amberly' })).toBeVisible();
  await guardians.getByRole('combobox', { name: 'Guardian' }).click();
  await page.getByRole('combobox', { name: 'Search by name or email' }).fill('sara dunfield');
  await page.getByRole('option', { name: /Sara Dunfield/ }).click();
  await guardians.getByRole('button', { name: 'Add guardian' }).click();
  await expect(page.getByText('Sara Dunfield is now a guardian of Ella Amberly.')).toBeVisible();
  await expect(guardians.getByRole('link', { name: 'Sara Dunfield' })).toBeVisible();

  const account = page.getByRole('region', { name: 'Account' });
  await account.getByRole('button', { name: 'Create account' }).click();
  const create = page.getByRole('dialog', { name: 'Create an account for Ella Amberly' });
  await create.getByLabel('Email').fill('ella.amberly@example.org');
  await create.getByRole('button', { name: 'Create and invite' }).click();
  await expect(create).toBeHidden();
  await expect(
    page.getByText(
      'Ella Amberly has an account now. The invitation has been sent to ella.amberly@example.org.',
    ),
  ).toBeVisible();
  await expect(account.getByText('Unconfirmed')).toBeVisible();

  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox.getByRole('article').filter({ hasText: 'ella.amberly@example.org' });
  await expect(mail).toHaveCount(1);
  await expect(mail).toContainText('has created an account for you');
  await expect(mail.getByRole('link', { name: /reset-password\?token=/ })).toBeVisible();
});

test('the office deletes a person added by mistake, but not one with family links', async ({
  page,
}) => {
  const office = await signIn(page, 'Office');
  await openAdministration(page, office);
  await openSection(page, 'Persons');

  await page.getByRole('button', { name: 'Add person' }).click();
  const add = page.getByRole('dialog', { name: 'Add a person' });
  await add.getByLabel('First name').fill('Quentin');
  await add.getByLabel('Last name').fill('Mistaken');
  await add.getByRole('button', { name: 'Add person' }).click();
  await expect(add).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'Quentin Mistaken' })).toBeVisible();

  await page.getByRole('button', { name: 'Delete Quentin Mistaken' }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Delete Quentin Mistaken?' });
  await confirm.getByRole('button', { name: 'Delete' }).click();
  await expect(page.getByText('Quentin Mistaken has been deleted.')).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: 'Persons' })).toBeVisible();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('mistaken');
  await expect(page.getByText('Nobody matches “mistaken”.')).toBeVisible();

  // A child with guardians stays.
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('ella amberly');
  await page.getByRole('link', { name: 'Ella Amberly' }).click();
  await page.getByRole('button', { name: 'Delete Ella Amberly' }).click();
  const refused = page.getByRole('alertdialog', { name: 'Delete Ella Amberly?' });
  await refused.getByRole('button', { name: 'Delete' }).click();
  await expect(refused.getByRole('alert')).toHaveText(
    'Ella Amberly still has an account, a staff link, bookings, class memberships, passes they own or paid for, guardians or children, so they cannot be deleted. Remove those first; passes are never deleted, so a person with passes stays.',
  );
});

/** The audit log: a table on wider screens, a list of cards on phones. */
const auditLog = (page: Page) =>
  page.getByRole('table', { name: 'Audit log' }).or(page.getByRole('list', { name: 'Audit log' }));

test('an admin signs in as a member, returns and finds both in the audit log', async ({ page }) => {
  await gotoDemo(page, '/login');
  const member = await demoName(page, 'Member');
  const admin = await signIn(page, 'Administrator');

  await page.getByRole('banner').getByRole('button', { name: admin }).click();
  await page.getByRole('menuitem', { name: 'Accounts' }).click();
  await page
    .getByRole('table', { name: 'Accounts' })
    .getByRole('button', { name: `Sign in as ${member}` })
    .click();
  const banner = page.getByRole('region', { name: 'Signed in as someone else' });
  await banner.getByRole('button', { name: 'Return to my account' }).click();
  await expect(banner).toBeHidden();

  await openAdministration(page, admin);
  await openSection(page, 'Audit log');

  // Newest first: the return, then the start.
  const log = auditLog(page);
  const entries = log.getByRole('row').or(log.getByRole('listitem')).filter({ hasText: member });
  await expect(entries).toHaveCount(2);
  await expect(entries.nth(0)).toContainText('Sign-in as ended');
  await expect(entries.nth(1)).toContainText('Sign-in as started');
  await expect(entries.nth(0)).toContainText(admin);

  await page.getByRole('searchbox', { name: 'Search the audit log' }).fill('started');
  await expect(entries).toHaveCount(1);
  await expect(entries).toContainText('Sign-in as started');
});

test('the office has no audit log', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openAdministration(page, office);
  await expect(
    page
      .getByRole('navigation', { name: 'Administration' })
      .getByRole('link', { name: 'Audit log' }),
  ).toHaveCount(0);

  await gotoDemo(page, '/admin/audit');
  await expect(page.getByRole('heading', { level: 1, name: 'Audit log' })).toBeVisible();
  await expect(page.getByText('Only administrators can see this page.')).toBeVisible();
  await expect(auditLog(page)).toHaveCount(0);
});
