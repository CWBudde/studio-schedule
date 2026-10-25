import { expect, type Page, signIn, signOut, test } from './fixtures';

// The demo uses passes on attendance (ADR 0012). Its member login, Clara
// Amberly, has a 10-visit pass the office sold her two weeks ago, used by
// two drop-ins it marked attended; her son Finn has no pass.

/** Opens a section of the administration from the account menu. */
async function openAdmin(page: Page, name: string, section: string, heading = section) {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: section })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
}

/** Opens a page of the main navigation. */
async function openMain(page: Page, link: string) {
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: link }).click();
  await expect(page.getByRole('heading', { level: 1, name: link })).toBeVisible();
}

/** The card of a pass in "My passes". */
const myPass = (page: Page, name: string) =>
  page.getByRole('region', { name: 'My passes' }).getByRole('listitem', { name, exact: true });

test('the office sells a pass to a child that the parent paid, and the family uses it', async ({
  page,
}) => {
  const office = await signIn(page, 'Office');

  // A new pass type.
  await openAdmin(page, office, 'Passes');
  await page.getByRole('button', { name: 'Add pass type' }).click();
  const add = page.getByRole('dialog', { name: 'Add a pass type' });
  await add.getByLabel('Name').fill('Holiday pass');
  await add.getByLabel('Visits').fill('5');
  await add.getByLabel('Valid for (days)').fill('60');
  await add.getByLabel('Price (EUR)').fill('45');
  await add.getByLabel('VAT rate (%)').fill('19');
  await add.getByRole('button', { name: 'Add pass type' }).click();
  await expect(add).toBeHidden();
  await expect(page.getByText('Holiday pass has been added.')).toBeVisible();

  // Sold to Finn, paid by his mother.
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Persons' })
    .click();
  await page.getByRole('searchbox', { name: 'Search persons' }).fill('finn amberly');
  await page.getByRole('link', { name: 'Finn Amberly' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Finn Amberly' })).toBeVisible();
  const passes = page.getByRole('region', { name: 'Passes' });
  await expect(passes.getByText('Finn Amberly has no passes.')).toBeVisible();
  await passes.getByRole('button', { name: 'Sell a pass' }).click();
  const sell = page.getByRole('dialog', { name: 'Sell a pass to Finn Amberly' });
  await sell.getByRole('combobox', { name: 'Pass type' }).click();
  await page.getByRole('option', { name: 'Holiday pass · €45.00' }).click();
  await sell.getByRole('combobox', { name: 'Paid by' }).click();
  await page.getByRole('combobox', { name: 'Search by name or email' }).fill('clara amberly');
  await page.getByRole('option', { name: /Clara Amberly/ }).click();
  await expect(sell.getByRole('combobox', { name: 'Paid by' })).toHaveText('Clara Amberly');
  await sell.getByRole('button', { name: 'Sell' }).click();
  await expect(sell).toBeHidden();
  await expect(page.getByText('Finn Amberly has bought Holiday pass.')).toBeVisible();
  const sold = passes.getByRole('list', { name: 'Passes of Finn Amberly' }).getByRole('listitem');
  await expect(sold).toHaveCount(1);
  await expect(sold.getByText('Usable')).toBeVisible();
  await expect(sold.getByText(/^5 of 5 visits left · /)).toBeVisible();
  await expect(sold.getByText(/for €45\.00 · paid by Clara Amberly$/)).toBeVisible();
  await signOut(page, office);

  // Clara sees it in My passes.
  await signIn(page, 'Member');
  await openMain(page, 'My lessons');
  const finns = myPass(page, 'Holiday pass, Finn Amberly');
  await expect(finns.getByText('Usable')).toBeVisible();
  await expect(finns.getByText('5 of 5 visits left')).toBeVisible();
  await expect(finns.getByText('Paid by Clara Amberly')).toBeVisible();
  await expect(finns.getByText('Not used yet.')).toBeVisible();
  // Nothing is wider than the page, on a phone too.
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true);

  // Booking a workshop for Finn says which pass it will use.
  await openMain(page, 'Schedule');
  await page.getByRole('button', { name: 'Next 14 days' }).click();
  const card = page
    .getByRole('main')
    .getByRole('button', { name: /\d{2}:\d{2}–\d{2}:\d{2}, Workshop$/ })
    .filter({ hasNotText: 'Booked' })
    .filter({ hasText: /\d+ free/ })
    .first();
  const courseType = ((await card.getAttribute('aria-label')) ?? '').split(', ')[0] ?? '';
  await card.click();
  const sheet = page.getByRole('dialog', { name: courseType });
  const booking = sheet.getByRole('region', { name: 'Booking' });
  await booking.getByRole('combobox', { name: 'Book for' }).click();
  await page.getByRole('option', { name: 'Finn Amberly' }).click();
  const hint = 'Will use Holiday pass when attended; 4 visits left after that.';
  await expect(booking.getByText(hint)).toBeVisible();
  await expect(
    booking.getByRole('button', { name: 'Book', exact: true }),
  ).toHaveAccessibleDescription(hint);
  await booking.getByRole('button', { name: 'Book', exact: true }).click();
  await expect(page.getByText('Finn Amberly is booked.')).toBeVisible();
});

test('the office takes attendance of a past lesson, and the pass pays for it', async ({ page }) => {
  const office = await signIn(page, 'Office');

  // Clara drops into a past ballet lesson, which her pass covers.
  await openMain(page, 'Schedule');
  await page.getByRole('button', { name: 'Previous 14 days' }).click();
  await page
    .getByRole('main')
    .getByRole('button', { name: /^Classical Ballet, / })
    .filter({ hasNotText: 'Canceled' })
    .last()
    .click();
  const sheet = page.getByRole('dialog', { name: 'Classical Ballet' });
  const section = sheet.getByRole('region', { name: 'Participants' });
  await section.getByRole('button', { name: 'Add participant' }).click();
  const add = page.getByRole('dialog', { name: 'Add participant' });
  await add.getByRole('combobox', { name: 'Person' }).click();
  await page.getByRole('combobox', { name: 'Search by name' }).fill('clara amberly');
  await page.getByRole('option', { name: /Clara Amberly/ }).click();
  await add.getByRole('button', { name: 'Add', exact: true }).click();
  await expect(add).toBeHidden();
  await expect(page.getByText('Clara Amberly has been added.')).toBeVisible();

  const clara = section
    .getByRole('list', { name: 'Booked participants' })
    .getByRole('listitem')
    .filter({ hasText: 'Clara Amberly' });
  // On attendance: nothing is used before it is taken.
  await expect(clara.getByText('Pending', { exact: true })).toBeVisible();
  await section.getByRole('button', { name: 'Clara Amberly attended' }).click();
  await expect(page.getByText('Clara Amberly attended.')).toBeVisible();
  await expect(clara.getByText('Paid · 10-visit pass, 7 left')).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await signOut(page, office);

  // Her pass shows one more use.
  await signIn(page, 'Member');
  await openMain(page, 'My lessons');
  const pass = myPass(page, '10-visit pass, Clara Amberly');
  await expect(pass.getByText('7 of 10 visits left')).toBeVisible();
  await expect(pass.getByText('3 uses in total')).toBeVisible();
  await expect(
    pass
      .getByRole('list', { name: 'Uses of 10-visit pass' })
      .getByRole('listitem')
      .filter({ hasText: 'Classical Ballet' }),
  ).toHaveCount(1);
});
