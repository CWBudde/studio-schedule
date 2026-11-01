import { expect, type Page, savedStore, signIn, test } from './fixtures';

/** The levels: a table on wider screens, a list of cards on phones. */
const levelsList = (page: Page) =>
  page.getByRole('table', { name: 'Levels' }).or(page.getByRole('list', { name: 'Levels' }));

/** The levels in the order the list shows them, from their edit buttons. */
async function levelOrder(page: Page): Promise<string[]> {
  const labels = await levelsList(page)
    .getByRole('button', { name: /^Edit / })
    .evaluateAll((buttons) => buttons.map((b) => b.getAttribute('aria-label') ?? ''));
  return labels.map((label) => label.replace(/^Edit /, ''));
}

/** Opens Admin → Levels from the account menu and the administration's navigation. */
async function openLevels(page: Page, name: string): Promise<void> {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Locations and rooms' })).toBeVisible();
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Levels' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Levels' })).toBeVisible();
}

test('the office adds a level, moves it up, and deletes it', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openLevels(page, office);
  await expect
    .poll(() => levelOrder(page))
    .toEqual(['Beginner', 'Intermediate', 'Advanced', 'Professional']);
  // The first level has nowhere to go up.
  await expect(levelsList(page).getByRole('button', { name: 'Move Beginner up' })).toHaveCount(0);

  // A new level goes after the last one.
  await page.getByRole('button', { name: 'Add level' }).click();
  const add = page.getByRole('dialog', { name: 'Add a level' });
  await add.getByLabel('Name').fill('Masterclass');
  await add.getByLabel('Code (optional)').fill('MC');
  await add.getByRole('button', { name: 'Add level' }).click();
  await expect(add).toBeHidden();
  await expect(page.getByText('Masterclass has been added.')).toBeVisible();
  await expect
    .poll(() => levelOrder(page))
    .toEqual(['Beginner', 'Intermediate', 'Advanced', 'Professional', 'Masterclass']);
  await expect(levelsList(page).getByRole('button', { name: 'Move Masterclass down' })).toHaveCount(
    0,
  );

  await levelsList(page).getByRole('button', { name: 'Move Masterclass up' }).click();
  await expect
    .poll(() => levelOrder(page))
    .toEqual(['Beginner', 'Intermediate', 'Advanced', 'Masterclass', 'Professional']);

  // The demo saves its data a moment after a change; the order stays after a reload.
  await expect
    .poll(() => savedStore(page), { timeout: 10_000 })
    .toMatch(/"Name":"Masterclass"[^}]*"Position":4\b/);
  await page.reload();
  await expect(page.getByText('Starting demo backend…')).toBeHidden({ timeout: 30_000 });
  await expect(page.getByRole('heading', { level: 1, name: 'Levels' })).toBeVisible();
  await expect
    .poll(() => levelOrder(page))
    .toEqual(['Beginner', 'Intermediate', 'Advanced', 'Masterclass', 'Professional']);

  await levelsList(page).getByRole('button', { name: 'Delete Masterclass' }).click();
  const remove = page.getByRole('alertdialog', { name: 'Delete Masterclass?' });
  await remove.getByRole('button', { name: 'Delete' }).click();
  await expect(remove).toBeHidden();
  await expect(page.getByText('Masterclass has been deleted.')).toBeVisible();
  await expect
    .poll(() => levelOrder(page))
    .toEqual(['Beginner', 'Intermediate', 'Advanced', 'Professional']);
});
