import { expect, gotoDemo, type Page, signIn, test } from './fixtures';

// The demo school is "Studio Demo — Ballet & Dance" with the app's own
// colors, a fictional legal notice, and lessons, prices and receipts, which
// fix its time zone and currency (ADR 0013).

/** Opens the settings page from the account menu. */
async function openSettings(page: Page, name: string) {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Settings' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible();
}

const primary = (page: Page) =>
  page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--primary').trim(),
  );

test('an admin renames the school and sets its accent color', async ({ page }) => {
  await gotoDemo(page, '/');
  await expect(page).toHaveTitle('Studio Demo — Ballet & Dance');
  await expect(
    page.getByRole('contentinfo').getByRole('region', { name: 'Legal notice' }),
  ).toContainText('Studio Demo Dance School');

  const name = await signIn(page, 'Administrator');
  await openSettings(page, name);

  const school = page.getByRole('form', { name: 'School' });
  await school.getByLabel('School name').fill('Sample Ballet Academy');
  await school.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('The settings have been saved.').first()).toBeVisible();

  await expect(
    page.getByRole('banner').getByRole('link', { name: 'Sample Ballet Academy – home' }),
  ).toBeVisible();
  await expect(page).toHaveTitle('Sample Ballet Academy');

  // Too light for the text on it: refused before it is sent.
  const branding = page.getByRole('form', { name: 'Branding' });
  const accent = branding.getByLabel('Accent color', { exact: true });
  await accent.fill('#ff0000');
  await expect(accent).toHaveAccessibleDescription(/below WCAG AA/);
  await branding.getByRole('button', { name: 'Save changes' }).click();
  await expect(branding.getByText(/This color is too light/)).toBeVisible();

  await accent.fill('#0f766e');
  await expect(accent).toHaveAccessibleDescription(/meets WCAG AA/);
  await branding.getByRole('button', { name: 'Save changes' }).click();
  await expect.poll(() => primary(page)).toBe('#0f766e');

  // The time zone is fixed by the demo's lessons.
  await expect(
    page.getByRole('form', { name: 'Time and language' }).getByLabel('Time zone'),
  ).toHaveAttribute('readonly', '');
});

// A real JPEG, which the server checks by its first bytes: the werkstatt demo school's.
const backgroundJpeg = '../internal/seed/assets/werkstatt-background.jpg';

test('an admin chooses the theme and uploads and removes a background image', async ({ page }) => {
  const name = await signIn(page, 'Administrator');
  await openSettings(page, name);

  const branding = page.getByRole('form', { name: 'Branding' });
  const theme = branding.getByRole('combobox', { name: 'Theme' });
  await expect(theme).toHaveText('Programme');
  await theme.click();
  await page.getByRole('option', { name: 'Werkstatt' }).click();
  await branding.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('The settings have been saved.').first()).toBeVisible();
  // The form shows the server's answer.
  await expect(theme).toHaveText('Werkstatt');
  await expect(theme).toHaveAccessibleDescription(/^Condensed type, colored day bands/);

  const card = page.getByRole('region', { name: 'Branding' });
  await expect(card.getByText('No background image.')).toBeVisible();
  await card.getByLabel('Upload a background image').setInputFiles(backgroundJpeg);
  await expect(page.getByText('The background image has been saved.')).toBeVisible();
  await expect(card.getByRole('img', { name: "The school's background image" })).toHaveAttribute(
    'src',
    /^data:image\/jpeg;base64,/,
  );

  await card.getByRole('button', { name: 'Remove the background image' }).click();
  await expect(page.getByText('The background image has been removed.')).toBeVisible();
  await expect(card.getByText('No background image.')).toBeVisible();
});

test('the office reads the settings without changing them', async ({ page }) => {
  const name = await signIn(page, 'Office');
  await openSettings(page, name);

  const school = page.getByRole('region', { name: 'School' });
  await expect(school.getByText('Studio Demo — Ballet & Dance')).toBeVisible();
  await expect(school.getByText('Only administrators can change this.')).toBeVisible();
  await expect(school.getByRole('form')).toHaveCount(0);

  const regional = page.getByRole('region', { name: 'Time and language' });
  await expect(regional.getByText('Europe/Berlin')).toBeVisible();
  await expect(regional.getByText(/^Fixed: lessons exist/)).toBeVisible();

  const rules = page.getByRole('region', { name: 'Booking rules' });
  await expect(rules.getByText('Release window')).toBeVisible();
  await expect(rules.getByRole('form')).toHaveCount(0);

  // Nothing is wider than the page, on a phone too.
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});
