import { expect, gotoDemo, hashSearch, type Page, test } from './fixtures';

/** Signs in with the demo account of a role from the login page; returns its name. */
async function signInAs(page: Page, role: string): Promise<string> {
  await gotoDemo(page, '/login');
  const button = page
    .getByRole('region', { name: 'Demo accounts' })
    .getByRole('button', { name: new RegExp(`^Sign in as .+ \\(${role}\\)$`) });
  const label = (await button.getAttribute('aria-label')) ?? '';
  const name = new RegExp(`^Sign in as (.+) \\(${role}\\)$`).exec(label)?.[1] ?? '';
  expect(name).not.toBe('');
  await button.click();
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
  return name;
}

/** Opens the administration from the account menu; it starts with the locations. */
async function openAdministration(page: Page, name: string): Promise<void> {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Administration' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Locations and rooms' })).toBeVisible();
}

/**
 * The saved store as text (JSON), or '' if nothing is saved yet. See
 * web/src/api/wasm/persistence.ts.
 */
function savedStore(page: Page): Promise<string> {
  return page.evaluate(async () => {
    // Opening a database that does not exist would create it empty.
    const dbs = await indexedDB.databases();
    if (!dbs.some((db) => db.name === 'studio-schedule-demo')) return '';
    return new Promise<string>((resolve) => {
      const open = indexedDB.open('studio-schedule-demo');
      open.onerror = () => resolve('');
      open.onsuccess = () => {
        const db = open.result;
        if (!db.objectStoreNames.contains('snapshot')) {
          db.close();
          resolve('');
          return;
        }
        const get = db.transaction('snapshot').objectStore('snapshot').get('current');
        get.onsuccess = () => {
          db.close();
          const store = (get.result as { store?: Uint8Array } | undefined)?.store;
          resolve(store ? new TextDecoder().decode(store) : '');
        };
        get.onerror = () => {
          db.close();
          resolve('');
        };
      };
    });
  });
}

/** Waits until the saved data contains `text`, i.e. a change has been saved. */
async function waitForSaved(page: Page, text: string): Promise<void> {
  await expect.poll(() => savedStore(page), { timeout: 10_000 }).toContain(text);
}

async function waitForBackend(page: Page): Promise<void> {
  await expect(page.getByText('Starting demo backend…')).toBeHidden({ timeout: 30_000 });
}

const elsewhere = (page: Page) =>
  page.getByRole('heading', { name: 'The demo is open in another tab' });

test('the demo keeps changes and the session across reloads until it is reset', async ({
  page,
}) => {
  const office = await signInAs(page, 'Office');
  await openAdministration(page, office);

  await page.getByRole('button', { name: 'Edit Studio North' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit Studio North' });
  await edit.getByLabel('Name').fill('Studio Lantern');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Studio Lantern has been saved.')).toBeVisible();
  await waitForSaved(page, 'Studio Lantern');

  // A reload restores the data and the session.
  await page.reload();
  await waitForBackend(page);
  await expect(page.getByRole('heading', { level: 1, name: 'Locations and rooms' })).toBeVisible();
  await expect(page.getByRole('banner').getByRole('button', { name: office })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit Studio Lantern' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit Studio North' })).toHaveCount(0);

  // Resetting asks first, then starts over from the sample data, signed out.
  await page.getByRole('banner').getByRole('button', { name: 'Reset demo' }).click();
  const confirm = page.getByRole('alertdialog', { name: 'Reset the demo?' });
  await expect(confirm).toContainText('signs you out');
  await confirm.getByRole('button', { name: 'Reset demo' }).click();

  await expect(page.getByRole('banner').getByRole('link', { name: 'Sign in' })).toBeVisible({
    timeout: 30_000,
  });
  await expect(page.getByRole('banner').getByRole('button', { name: office })).toHaveCount(0);

  await signInAs(page, 'Office');
  await openAdministration(page, office);
  await expect(page.getByRole('button', { name: 'Edit Studio North' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Edit Studio Lantern' })).toHaveCount(0);
});

test('lessons generated in the background keep their links across a reload', async ({ page }) => {
  // Lessons get random IDs when the job generates them; the job's changes
  // are saved like any other, so the link still works after a reload.
  await gotoDemo(page, '/schedule');
  await page
    .getByRole('main')
    .getByRole('button', { name: /\d{2}:\d{2}–/ })
    .first()
    .click();
  const lesson = hashSearch(page).get('lesson') ?? '';
  expect(lesson).not.toBe('');
  await waitForSaved(page, lesson);

  await page.reload();
  await waitForBackend(page);
  await expect(page.getByRole('dialog').getByText('Capacity', { exact: true })).toBeVisible();
});

test('one tab runs the demo; "Use here" moves it with its changes and session', async ({
  page,
  context,
}) => {
  const office = await signInAs(page, 'Office');
  await openAdministration(page, office);
  await page.getByRole('button', { name: 'Edit Studio North' }).click();
  const edit = page.getByRole('dialog', { name: 'Edit Studio North' });
  await edit.getByLabel('Name').fill('Studio Lantern');
  await edit.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText('Studio Lantern has been saved.')).toBeVisible();

  // A second tab finds the demo busy.
  const other = await context.newPage();
  await other.goto(page.url());
  await expect(elsewhere(other)).toBeVisible({ timeout: 30_000 });
  await expect(other.getByRole('button', { name: 'Reset demo' })).toHaveCount(0);

  // "Use here": the first tab saves its last change and hands over.
  await other.getByRole('button', { name: 'Use here' }).click();
  await waitForBackend(other);
  await expect(other.getByRole('heading', { level: 1, name: 'Locations and rooms' })).toBeVisible();
  await expect(other.getByRole('banner').getByRole('button', { name: office })).toBeVisible();
  await expect(other.getByRole('button', { name: 'Edit Studio Lantern' })).toBeVisible();
  await expect(elsewhere(page)).toBeVisible();

  // And back.
  await page.getByRole('button', { name: 'Use here' }).click();
  await waitForBackend(page);
  await expect(page.getByRole('button', { name: 'Edit Studio Lantern' })).toBeVisible();
  await expect(elsewhere(other)).toBeVisible();
  await other.close();
});
