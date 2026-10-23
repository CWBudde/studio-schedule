import { expect, gotoDemo, test } from './fixtures';

test('signs in with a demo account from the login page and out again', async ({ page }) => {
  await gotoDemo(page, '/login');
  const header = page.getByRole('banner');
  await expect(header.getByRole('link', { name: 'Sign in' })).toBeVisible();

  const accounts = page.getByRole('region', { name: 'Demo accounts' });
  const member = accounts.getByRole('button', { name: /^Sign in as .+ \(Member\)$/ });
  const label = (await member.getAttribute('aria-label')) ?? '';
  const name = /^Sign in as (.+) \(Member\)$/.exec(label)?.[1] ?? '';
  expect(name).not.toBe('');
  await member.click();

  const account = header.getByRole('button', { name });
  await expect(account).toBeVisible();
  expect(new URL(page.url()).hash).toBe('#/');

  await account.click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();

  await expect(header.getByRole('link', { name: 'Sign in' })).toBeVisible();
  await expect(account).toHaveCount(0);
});

test('registers, follows the link from the demo mailbox and ends up signed in', async ({
  page,
}) => {
  const password = 'first-steps-2026';
  await gotoDemo(page, '/register');

  await page.getByLabel('First name').fill('Nina');
  await page.getByLabel('Last name').fill('Dancer');
  await page.getByLabel('Email').fill('new.dancer@example.org');
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Register' }).click();
  await expect(page.getByText('Check your inbox')).toBeVisible();

  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox.getByRole('article').filter({ hasText: 'new.dancer@example.org' });
  await expect(mail).toHaveCount(1);
  await mail.getByRole('link', { name: /confirm\?token=/ }).click();

  await expect(mailbox).toBeHidden();
  await expect(
    page.getByRole('heading', { level: 1, name: 'Confirm your email address' }),
  ).toBeVisible();
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Confirm and sign in' }).click();

  await expect(page.getByRole('banner').getByRole('button', { name: 'Nina Dancer' })).toBeVisible();
  expect(new URL(page.url()).hash).toBe('#/');
});
