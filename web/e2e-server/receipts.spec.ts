import { accounts, expect, signIn, test } from './fixtures';

test('the office sells a pass and downloads its receipt as a PDF', async ({ page }) => {
  // The first receipt compiles the typst renderer (wazero), unless its cache
  // survived from an earlier run.
  test.setTimeout(240_000);
  const { name } = accounts.office;
  await signIn(page, accounts.office);

  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Persons' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Persons' })).toBeVisible();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('kai lanternby');
  await page.getByRole('link', { name: 'Kai Lanternby' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Kai Lanternby' })).toBeVisible();

  const passes = page.getByRole('region', { name: 'Passes' });
  const receiptLinks = passes.getByRole('link', { name: /^Receipt of 30-day pass of / });
  // Kai has a 30-day pass already, and more after an earlier run.
  await expect(receiptLinks.first()).toBeVisible();
  const before = await receiptLinks.count();
  await passes.getByRole('button', { name: 'Sell a pass' }).click();
  const sell = page.getByRole('dialog', { name: 'Sell a pass to Kai Lanternby' });
  await sell.getByRole('combobox', { name: 'Pass type' }).click();
  await page.getByRole('option', { name: '30-day pass · €89.00' }).click();
  await expect(sell.getByRole('combobox', { name: 'Payment method' })).toHaveText('Cash');
  await sell.getByRole('button', { name: 'Sell' }).click();
  await expect(sell).toBeHidden({ timeout: 180_000 });
  await expect(page.getByText('Kai Lanternby has bought 30-day pass.')).toBeVisible();
  await expect(receiptLinks).toHaveCount(before + 1);

  // The new receipt, the first of the pass list.
  await receiptLinks.first().click();
  const sheet = page.getByRole('dialog', { name: /^Receipt SD-\d{4}-\d{4}$/ });
  await expect(sheet.getByText('Cash')).toBeVisible();
  const number = /Receipt (SD-\d{4}-\d{4})/.exec(
    (await sheet.getByRole('heading').first().textContent()) ?? '',
  )?.[1];
  expect(number).toBeDefined();

  // The web app opens the PDF in a new tab, where the browser downloads it
  // (Content-Disposition: attachment) with the session cookie.
  const pdf = page
    .context()
    .waitForEvent('response', (r) => /\/api\/v1\/admin\/receipts\/[^/]+\/pdf$/.test(r.url()));
  await sheet.getByRole('button', { name: `Download receipt ${number} as PDF` }).click();
  const answer = await pdf;
  expect(answer.status()).toBe(200);
  expect(answer.headers()['content-type']).toBe('application/pdf');
  expect(answer.headers()['content-disposition']).toMatch(/^attachment; filename=".+\.pdf"$/);

  // Its content, fetched again with the session of the page.
  const res = await page.request.get(answer.url());
  expect(res.status()).toBe(200);
  expect(res.headers()['content-type']).toBe('application/pdf');
  const body = await res.body();
  expect(body.length).toBeGreaterThan(1_000);
  expect(body.subarray(0, 5).toString('latin1')).toBe('%PDF-');
});
