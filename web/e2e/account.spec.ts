import {
  demoEmail,
  demoName,
  expect,
  gotoDemo,
  type Page,
  signIn,
  signOut,
  test,
} from './fixtures';

/** Signs in with the demo member from the login page; returns its name and email. */
async function signInAsMember(page: Page): Promise<{ name: string; email: string }> {
  await gotoDemo(page, '/login');
  const email = await demoEmail(page, 'Member');
  return { name: await signIn(page, 'Member'), email };
}

/** Opens the account settings from the account menu in the header. */
async function openAccount(page: Page, name: string): Promise<void> {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Account settings' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
}

test('changes the password from the account settings', async ({ page }) => {
  const password = 'changed-on-purpose-2026';
  const { name, email } = await signInAsMember(page);
  await openAccount(page, name);

  const card = page.getByRole('region', { name: 'Change password' });
  await card.getByLabel('Current password').fill('wrong-password');
  await card.getByLabel('New password', { exact: true }).fill(password);
  await card.getByLabel('Repeat new password').fill(password);
  await card.getByRole('button', { name: 'Change password' }).click();
  await expect(card.getByText('The password is incorrect.')).toBeVisible();

  await card.getByLabel('Current password').fill('demo-password');
  await card.getByRole('button', { name: 'Change password' }).click();
  await expect(
    card.getByText('Your password has been changed. Your other devices have been signed out.'),
  ).toBeVisible();
  // This session stays.
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();

  await signOut(page, name);

  await page.getByRole('banner').getByRole('link', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in' })).toBeVisible();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
});

test('changes the email address with the link from the demo mailbox', async ({ page }) => {
  const newEmail = 'moved.member@example.org';
  const { name } = await signInAsMember(page);
  await openAccount(page, name);

  const card = page.getByRole('region', { name: 'Change email address' });
  await card.getByLabel('New email address').fill(newEmail);
  await card.getByLabel('Password', { exact: true }).fill('demo-password');
  await card.getByRole('button', { name: 'Send confirmation link' }).click();
  await expect(
    card.getByText(`We have sent a link to ${newEmail}. Your address changes once you open it.`),
  ).toBeVisible();

  await card.getByRole('button', { name: 'Open the demo mailbox' }).click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox.getByRole('article').filter({ hasText: newEmail });
  await expect(mail).toHaveCount(1);
  await mail.getByRole('link', { name: /confirm-email-change\?token=/ }).click();

  await expect(mailbox).toBeHidden();
  await page.getByRole('button', { name: 'Confirm new address' }).click();
  await expect(page.getByText(`Your account now uses ${newEmail}.`)).toBeVisible();

  await page.getByRole('link', { name: 'Go to your account' }).click();
  await expect(
    page.getByRole('region', { name: 'Change email address' }).getByText(newEmail),
  ).toBeVisible();
});

test('edits the person data and shows the new name in the header', async ({ page }) => {
  const { name } = await signInAsMember(page);
  await openAccount(page, name);

  const card = page.getByRole('region', { name: 'Your details' });
  const givenName = card.getByLabel('First name');
  await expect(givenName).toHaveValue(name.split(' ')[0] ?? '');
  await givenName.fill('Marigold');
  await card.getByRole('button', { name: 'Save details' }).click();
  await expect(page.getByText('Your details have been saved.')).toBeVisible();

  const newName = name.replace(/^\S+/, 'Marigold');
  await expect(page.getByRole('banner').getByRole('button', { name: newName })).toBeVisible();
});

test('adds, edits and removes a child', async ({ page }) => {
  const { name } = await signInAsMember(page);
  await openAccount(page, name);
  const familyName = name.split(' ').slice(1).join(' ');
  const children = page.getByRole('region', { name: 'Your children' });
  const entry = (childName: string) =>
    children.getByRole('listitem').filter({ has: page.getByText(childName, { exact: true }) });

  await children.getByRole('button', { name: 'Add child' }).click();
  const add = page.getByRole('dialog', { name: 'Add a child' });
  await expect(add.getByLabel('Last name')).toHaveValue(familyName);
  await add.getByLabel('First name').fill('Wren');
  await add.getByLabel('Birth date (optional)').fill('2020-02-29');
  await add.getByRole('button', { name: 'Add child' }).click();
  await expect(add).toBeHidden();
  await expect(entry(`Wren ${familyName}`)).toContainText('Born February 29, 2020');

  await entry(`Wren ${familyName}`)
    .getByRole('button', { name: `Edit Wren ${familyName}` })
    .click();
  const edit = page.getByRole('dialog', { name: `Edit Wren ${familyName}` });
  await expect(edit.getByLabel('Birth date (optional)')).toHaveValue('2020-02-29');
  await edit.getByLabel('First name').fill('Wrenna');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(edit).toBeHidden();
  const wrenna = entry(`Wrenna ${familyName}`);
  await expect(wrenna).toBeVisible();
  await expect(entry(`Wren ${familyName}`)).toHaveCount(0);

  await wrenna.getByRole('button', { name: `Remove Wrenna ${familyName}` }).click();
  const confirm = wrenna.getByRole('group', {
    name: `Remove Wrenna ${familyName} from your family?`,
  });
  await expect(confirm).toContainText(
    'Wrenna has no other guardian, so the details of Wrenna are deleted.',
  );
  await confirm.getByRole('button', { name: 'Yes, remove' }).click();
  await expect(wrenna).toHaveCount(0);
  await expect(
    page.getByText(`Wrenna ${familyName} has been removed from your family.`),
  ).toBeVisible();
});

test('removing a child with another guardian only takes it out of the family', async ({ page }) => {
  const { name } = await signInAsMember(page);
  await openAccount(page, name);
  const children = page.getByRole('region', { name: 'Your children' });
  const shared = children
    .getByRole('listitem')
    .filter({ hasText: 'Also in the family of ' })
    .first();
  const removeButton = shared.getByRole('button', { name: /^Remove / });
  const childName = ((await removeButton.getAttribute('aria-label')) ?? '').replace(/^Remove /, '');
  const guardians = (
    (await shared.getByText(/^Also in the family of /).textContent()) ?? ''
  ).replace('Also in the family of ', '');
  expect(childName).not.toBe('');
  expect(guardians).not.toBe('');

  await removeButton.click();
  const confirm = page.getByRole('group', { name: `Remove ${childName} from your family?` });
  await expect(confirm).toContainText(
    `${childName.split(' ')[0]} stays registered with ${guardians} and is only removed from your family.`,
  );
  await confirm.getByRole('button', { name: 'Yes, remove' }).click();

  await expect(page.getByText(`${childName} has been removed from your family.`)).toBeVisible();
  await expect(children.getByText(childName, { exact: true })).toHaveCount(0);
});

test('signs in in the language chosen for emails', async ({ page }) => {
  const { name, email } = await signInAsMember(page);
  await openAccount(page, name);

  const card = page.getByRole('region', { name: 'Your details' });
  await card.getByRole('combobox', { name: 'Language for emails' }).click();
  await page.getByRole('option', { name: 'Deutsch' }).click();
  await expect(page.getByText('The language for emails has been saved.')).toBeVisible();
  // Choosing it does not switch the UI.
  await expect(page.getByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();

  await signOut(page, name);
  await page.getByRole('banner').getByRole('link', { name: 'Sign in' }).click();
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('demo-password');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();

  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Sprache ändern' })).toBeVisible();
});

test('a child with an account of their own is read-only and only unlinked', async ({ page }) => {
  await gotoDemo(page, '/login');
  const member = await demoName(page, 'Member');

  // The office creates an account for a child of the member.
  const office = await signIn(page, 'Office');
  await page.getByRole('banner').getByRole('button', { name: office }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Persons' })
    .click();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill(member);
  await page.getByRole('link', { name: member }).click();
  const child = page.getByRole('region', { name: 'Children' }).getByRole('link').first();
  const childName = ((await child.textContent()) ?? '').trim();
  expect(childName).not.toBe('');
  await child.click();
  await expect(page.getByRole('heading', { level: 1, name: childName })).toBeVisible();
  await page
    .getByRole('region', { name: 'Account' })
    .getByRole('button', { name: 'Create account' })
    .click();
  const create = page.getByRole('dialog', { name: `Create an account for ${childName}` });
  await create.getByLabel('Email').fill(`${childName.replace(' ', '.').toLowerCase()}@example.net`);
  await create.getByRole('button', { name: 'Create and invite' }).click();
  await expect(create).toBeHidden();

  // The member sees the child read-only and can only unlink it.
  await signOut(page, office);
  await signIn(page, 'Member');
  await openAccount(page, member);
  const entry = page
    .getByRole('region', { name: 'Your children' })
    .getByRole('listitem')
    .filter({ has: page.getByText(childName, { exact: true }) });
  const givenName = childName.split(' ')[0] ?? '';
  await expect(entry).toContainText(
    `These details are maintained by ${givenName} or the office, so you cannot change them.`,
  );
  await expect(entry.getByRole('button', { name: `Edit ${childName}` })).toHaveCount(0);

  await entry.getByRole('button', { name: `Remove ${childName}` }).click();
  const confirm = entry.getByRole('group', { name: `Remove ${childName} from your family?` });
  await expect(confirm).toContainText(
    `${givenName} stays registered and is only removed from your family.`,
  );
  await confirm.getByRole('button', { name: 'Yes, remove' }).click();
  await expect(page.getByText(`${childName} has been removed from your family.`)).toBeVisible();
  await expect(entry).toHaveCount(0);
});
