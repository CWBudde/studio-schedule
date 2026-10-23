import { expect, gotoDemo, type Page, test } from './fixtures';

/** The name and email of the demo member, from the demo accounts on the login page. */
async function demoMember(page: Page): Promise<{ name: string; email: string }> {
  const accounts = page.getByRole('region', { name: 'Demo accounts' });
  const member = page.getByRole('button', { name: /^Sign in as .+ \(Member\)$/ });
  const button = accounts.locator(member);
  const label = (await button.getAttribute('aria-label')) ?? '';
  const name = /^Sign in as (.+) \(Member\)$/.exec(label)?.[1] ?? '';
  const row = accounts.getByRole('listitem').filter({ has: member });
  const email = ((await row.getByRole('definition').first().textContent()) ?? '').trim();
  expect(name).not.toBe('');
  expect(email).toContain('@');
  return { name, email };
}

/** Signs in with the login form (not a demo account button). */
async function signIn(page: Page, email: string, password: string): Promise<void> {
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
}

test('resets a forgotten password from the demo mailbox and signs in with the new one', async ({
  page,
}) => {
  const password = 'a-fresh-start-2026';
  await gotoDemo(page, '/login');
  const { name, email } = await demoMember(page);

  await page.getByRole('link', { name: 'Forgot password?' }).click();
  await expect(
    page.getByRole('heading', { level: 1, name: 'Forgot your password?' }),
  ).toBeVisible();
  await page.getByLabel('Email').fill(email);
  await page.getByRole('button', { name: 'Send link' }).click();
  await expect(page.getByRole('status').getByText('Check your inbox')).toBeVisible();

  await page.getByRole('button', { name: 'Open the demo mailbox' }).click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox
    .getByRole('article')
    .filter({ hasText: email })
    .filter({ hasText: 'reset-password?token=' });
  await expect(mail).toHaveCount(1);
  await mail.getByRole('link', { name: /reset-password\?token=/ }).click();

  await expect(mailbox).toBeHidden();
  await expect(page.getByRole('heading', { level: 1, name: 'Set a new password' })).toBeVisible();
  await page.getByLabel('New password').fill(password);
  await page.getByLabel('Repeat password').fill(password);
  await page.getByRole('button', { name: 'Set password' }).click();

  await expect(
    page.getByRole('heading', { level: 1, name: 'Your password has been changed' }),
  ).toBeVisible();
  await page.getByRole('main').getByRole('link', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in' })).toBeVisible();

  // The old password is gone.
  await signIn(page, email, 'demo-password');
  await expect(page.getByText('The email address or password is incorrect.')).toBeVisible();

  await signIn(page, email, password);
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
  expect(new URL(page.url()).hash).toBe('#/');
});

test('a reset link works only once', async ({ page }) => {
  await gotoDemo(page, '/login');
  const { email } = await demoMember(page);

  await page.getByRole('link', { name: 'Forgot password?' }).click();
  await expect(
    page.getByRole('heading', { level: 1, name: 'Forgot your password?' }),
  ).toBeVisible();
  await page.getByLabel('Email').fill(email);
  await page.getByRole('button', { name: 'Send link' }).click();
  await expect(page.getByRole('status').getByText('Check your inbox')).toBeVisible();

  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const resetLink = mailbox.getByRole('link', { name: /reset-password\?token=/ });
  const setPassword = async (password: string) => {
    await page.getByLabel('New password').fill(password);
    await page.getByLabel('Repeat password').fill(password);
    await page.getByRole('button', { name: 'Set password' }).click();
  };

  await page.getByRole('button', { name: 'Open the demo mailbox' }).click();
  await resetLink.click();
  await setPassword('a-fresh-start-2026');
  await expect(
    page.getByRole('heading', { level: 1, name: 'Your password has been changed' }),
  ).toBeVisible();
  await page.getByRole('main').getByRole('link', { name: 'Sign in' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in' })).toBeVisible();

  // The same link again.
  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  await resetLink.click();
  await setPassword('another-start-2026');

  await expect(
    page.getByRole('heading', { level: 1, name: 'This link is invalid or has expired' }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Request a new link' }).click();
  await expect(
    page.getByRole('heading', { level: 1, name: 'Forgot your password?' }),
  ).toBeVisible();
});
