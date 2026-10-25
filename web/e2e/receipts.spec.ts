import { readFile } from 'node:fs/promises';
import type { Download } from '@playwright/test';
import { expect, type Page, signIn, test } from './fixtures';

// The demo's receipt settings are complete (standard VAT, prefix SD), and
// the office sold Clara Amberly (the member login) her own 10-visit pass
// and the one of her daughter Ella; Kai Lanternby has a 30-day pass.

/** Opens a section of the administration from the account menu. */
async function openAdmin(page: Page, name: string, section: string) {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: section })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: section })).toBeVisible();
}

/** Clicks `button` and checks that it downloads a PDF. */
async function expectPdfDownload(page: Page, click: () => Promise<void>) {
  const [download] = await Promise.all([
    page.waitForEvent('download', { timeout: 60_000 }) as Promise<Download>,
    click(),
  ]);
  expect(download.suggestedFilename()).toMatch(/\.pdf$/);
  const bytes = await readFile((await download.path())!);
  expect(bytes.subarray(0, 4).toString('latin1')).toBe('%PDF');
}

test('the office sells a pass with a receipt, downloads it and cancels it', async ({ page }) => {
  test.setTimeout(120_000);
  const office = await signIn(page, 'Office');

  // Sold in cash to Kai, who is the recipient.
  await openAdmin(page, office, 'Persons');
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('kai lanternby');
  await page.getByRole('link', { name: 'Kai Lanternby' }).click();
  const passes = page.getByRole('region', { name: 'Passes' });
  await passes.getByRole('button', { name: 'Sell a pass' }).click();
  const sell = page.getByRole('dialog', { name: 'Sell a pass to Kai Lanternby' });
  await sell.getByRole('combobox', { name: 'Pass type' }).click();
  await page.getByRole('option', { name: '30-day pass · €89.00' }).click();
  await expect(sell.getByRole('combobox', { name: 'Payment method' })).toHaveText('Cash');
  await expect(sell.getByLabel('Name on the receipt')).toHaveValue('Kai Lanternby');
  await sell.getByRole('button', { name: 'Sell' }).click();
  await expect(sell).toBeHidden();
  await expect(page.getByText('Kai Lanternby has bought 30-day pass.')).toBeVisible();

  // Its receipt, from the pass.
  await passes
    .getByRole('link', { name: /^Receipt of 30-day pass of / })
    .first()
    .click();
  const sheet = page.getByRole('dialog', { name: /^Receipt SD-\d{4}-\d{4}$/ });
  await expect(sheet.getByText('Cash')).toBeVisible();
  await expect(sheet.getByText(/^Kai Lanternby/)).toBeVisible();
  await expect(sheet.getByText('Total').locator('xpath=following-sibling::dd[1]')).toHaveText(
    '€89.00',
  );
  const number = /Receipt (SD-\d{4}-\d{4})/.exec(
    (await sheet.getByRole('heading').first().textContent()) ?? '',
  )?.[1];
  expect(number).toBeDefined();

  // The PDF, as issued.
  await expectPdfDownload(page, () =>
    sheet.getByRole('button', { name: `Download receipt ${number} as PDF` }).click(),
  );

  // Kai got it by mail, with the PDF attached.
  await sheet.getByRole('button', { name: 'Close' }).click();
  await page
    .getByRole('banner')
    .getByRole('button', { name: /Demo mailbox/ })
    .click();
  const mailbox = page.getByRole('dialog', { name: 'Demo mailbox' });
  const mail = mailbox.getByRole('article').filter({ hasText: number! }).first();
  await expect(mail).toContainText(/kai\.lanternby@/i);
  await expect(mail.getByRole('button', { name: /^Open the attachment .+\.pdf$/ })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(mailbox).toBeHidden();

  // Canceled: the pass is voided.
  await page
    .getByRole('main')
    .getByRole('button', { name: `Open receipt ${number}` })
    .click();
  const again = page.getByRole('dialog', { name: `Receipt ${number}` });
  await again.getByRole('button', { name: 'Cancel receipt' }).click();
  const confirm = page.getByRole('alertdialog', { name: `Cancel receipt ${number}?` });
  await confirm.getByLabel('Reason (optional, printed)').fill('Sold by mistake');
  await confirm.getByRole('button', { name: 'Cancel receipt' }).click();
  await expect(
    page.getByText(/^Cancellation SD-\d{4}-\d{4} has been issued; the pass is voided\.$/),
  ).toBeVisible();
  const storno = page.getByRole('dialog', { name: /^Cancellation SD-\d{4}-\d{4}$/ });
  await expect(storno.getByText('Sold by mistake')).toBeVisible();
  await expect(storno.getByRole('button', { name: new RegExp(`^${number} of `) })).toBeVisible();
  await storno.getByRole('button', { name: 'Close' }).click();

  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Persons' })
    .click();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('kai lanternby');
  await page.getByRole('link', { name: 'Kai Lanternby' }).click();
  await expect(
    page
      .getByRole('region', { name: 'Passes' })
      .getByRole('listitem')
      .filter({ hasText: 'Voided' }),
  ).toHaveCount(1);
});

test('a member downloads the receipt of what she paid', async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page, 'Member');
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'My lessons' })
    .click();

  const receipts = page.getByRole('region', { name: 'My receipts' });
  // Her own pass and her daughter's.
  await expect(receipts.getByRole('listitem')).toHaveCount(2);
  await expect(receipts.getByText(/10-visit pass/).first()).toBeVisible();
  await expectPdfDownload(page, () =>
    receipts
      .getByRole('button', { name: /^Download receipt SD-\d{4}-\d{4} as PDF$/ })
      .first()
      .click(),
  );
});
