import { expect, expectPdfDownload, gotoDemo, signIn, test } from './fixtures';

const intensive = 'Contemporary Weekend Intensive';
const intensivePath = '/events/contemporary-weekend-intensive';

test('a member browses events and registers and cancels the last place for a dependent', async ({
  page,
}) => {
  await gotoDemo(page, '/events');
  const main = page.getByRole('main');
  await expect(main.getByRole('link', { name: /Open Ballet Taster/ })).toContainText('Free');
  await expect(main.getByRole('link', { name: /Summer Showcase Rehearsal/ })).toHaveCount(0);
  await main.getByRole('link', { name: new RegExp(intensive) }).click();
  await expect(main.getByRole('heading', { level: 1, name: intensive })).toBeVisible();
  await expect(main.getByText(/Saturday.+16:00–18:30/)).toBeVisible();
  await expect(main.getByText('€120.00', { exact: true })).toBeVisible();
  await expect(main.getByText('1 place left', { exact: true })).toBeVisible();

  await signIn(page, 'Member');
  await gotoDemo(page, intensivePath);
  await main.getByLabel('Participant').selectOption({ label: 'Finn Amberly' });
  await main.getByRole('button', { name: 'Register', exact: true }).click();
  await expect(main.getByText('Registered', { exact: true })).toBeVisible();
  await expect(main.getByText('Full', { exact: true })).toBeVisible();

  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  await expect(
    mailbox.getByRole('article').filter({ hasText: intensive }).filter({ hasText: 'Finn Amberly' }),
  ).toBeVisible();
  await mailbox.getByRole('button', { name: 'Close' }).click();

  await main.getByRole('button', { name: 'Cancel registration' }).click();
  const dialog = page.getByRole('alertdialog', { name: 'Cancel event registration?' });
  await dialog.getByRole('button', { name: 'Confirm cancellation' }).click();
  await expect(dialog).toBeHidden();
  await expect(main.getByText('1 place left', { exact: true })).toBeVisible();
});

test('the office records an event fee and downloads the receipt', async ({ page }) => {
  test.setTimeout(90_000);
  const office = await signIn(page, 'Office');
  await page.getByRole('banner').getByRole('button', { name: office }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Events' })
    .click();
  await page.getByRole('main').getByRole('link', { name: intensive, exact: true }).click();

  const row = page
    .getByRole('table', { name: 'Registrations' })
    .getByRole('row', { name: /Kai Lanternby/ });
  await row.getByRole('button', { name: 'Mark paid' }).click();
  const dialog = page.getByRole('dialog', { name: 'Mark paid' });
  await expect(dialog.getByRole('combobox', { name: 'Payment method' })).toContainText('Cash');
  await dialog.getByRole('button', { name: 'Mark paid' }).click();
  await expect(dialog).toBeHidden();
  await expect(row.getByRole('cell', { name: 'Paid', exact: true })).toBeVisible();
  await expectPdfDownload(page, () =>
    row.getByRole('button', { name: 'Download receipt' }).click(),
  );
});

test.describe('Werkstatt events', () => {
  test.use({ demoSchool: 'werkstatt' });

  test('uses its own slots for the public list and detail', async ({ page, context }) => {
    await context.addInitScript(() => localStorage.setItem('studio-schedule.lang', 'en'));
    await gotoDemo(page, '/events');
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'werkstatt');
    const events = page.locator('.werkstatt-events');
    await expect(events.getByRole('link', { name: /Ballett zum Ausprobieren/ })).toBeVisible();
    await events.getByRole('link', { name: /Contemporary-Intensivwochenende/ }).click();
    const detail = page.locator('.werkstatt-event');
    await expect(
      detail.getByRole('heading', { level: 1, name: 'Contemporary-Intensivwochenende' }),
    ).toBeVisible();
    await expect(detail.getByRole('heading', { level: 2, name: 'Sessions' })).toBeVisible();
  });
});
