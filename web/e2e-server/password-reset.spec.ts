import { accounts, expect, signIn, test, waitForMail } from './fixtures';

test('resets a forgotten password with the link from the mail and signs in', async ({ page }) => {
  // The server delivers its outbox once a minute.
  test.setTimeout(180_000);
  const { email, name } = accounts.reset;
  // New on every run, so the test passes again on the same database.
  const password = `smoke-reset-${Date.now()}`;
  const since = new Date(Date.now() - 1_000);

  await page.goto('/login');
  await page.getByRole('link', { name: 'Forgot password?' }).click();
  await expect(
    page.getByRole('heading', { level: 1, name: 'Forgot your password?' }),
  ).toBeVisible();
  await page.getByLabel('Email').fill(email);
  await page.getByRole('button', { name: 'Send link' }).click();
  await expect(page.getByRole('status').getByText('Check your inbox')).toBeVisible();

  const text = await waitForMail(email, since);
  const link = /https?:\/\/\S+reset-password\?token=[\w-]+/.exec(text)?.[0];
  expect(link, 'the reset link in the mail').toBeDefined();
  // The link points to BASE_URL, without the hash routing of the demo.
  expect(new URL(link!).origin).toBe(new URL(page.url()).origin);

  await page.goto(link!);
  await expect(page.getByRole('heading', { level: 1, name: 'Set a new password' })).toBeVisible();
  await page.getByLabel('New password').fill(password);
  await page.getByLabel('Repeat password').fill(password);
  await page.getByRole('button', { name: 'Set password' }).click();
  await expect(
    page.getByRole('heading', { level: 1, name: 'Your password has been changed' }),
  ).toBeVisible();

  await signIn(page, { email, name }, password);
});
