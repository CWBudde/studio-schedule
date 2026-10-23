import { expect, gotoDemo, type Page, test } from './fixtures';

/** Signs in with the demo member from the login page; returns its name and email. */
async function signInAsMember(page: Page): Promise<{ name: string; email: string }> {
  await gotoDemo(page, '/login');
  const accounts = page.getByRole('region', { name: 'Demo accounts' });
  const member = page.getByRole('button', { name: /^Sign in as .+ \(Member\)$/ });
  const button = accounts.locator(member);
  const label = (await button.getAttribute('aria-label')) ?? '';
  const name = /^Sign in as (.+) \(Member\)$/.exec(label)?.[1] ?? '';
  const row = accounts.getByRole('listitem').filter({ has: member });
  const email = ((await row.getByRole('definition').first().textContent()) ?? '').trim();
  expect(name).not.toBe('');
  await button.click();
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
  return { name, email };
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

  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page.getByRole('banner').getByRole('link', { name: 'Sign in' })).toBeVisible();

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
