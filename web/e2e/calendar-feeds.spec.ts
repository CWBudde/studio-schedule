import { readFile } from 'node:fs/promises';
import type { Locator } from '@playwright/test';
import { expect, gotoDemo, type Page, signIn, test } from './fixtures';

/**
 * Creates the link of the open "Subscribe" dialog and downloads the feed:
 * the demo has no server a calendar app could reach, so the download is all
 * it offers. Returns the file's name and content.
 */
async function createAndDownload(
  page: Page,
  dialog: Locator,
): Promise<{ filename: string; content: string }> {
  await dialog.getByRole('button', { name: 'Create link' }).click();
  const created = dialog.getByRole('region', { name: 'Your new link' });
  await expect(created.getByText(/calendar apps cannot reach this link/)).toBeVisible();
  await expect(created.getByRole('button', { name: 'Copy link' })).toHaveCount(0);

  const download = page.waitForEvent('download');
  await created.getByRole('button', { name: 'Download .ics' }).click();
  const file = await download;
  const path = await file.path();
  // Unfolded (RFC 5545 folds lines at 75 octets), so that names are whole.
  const content = (await readFile(path, 'utf8')).replace(/\r\n[ \t]/g, '');
  return { filename: file.suggestedFilename(), content };
}

test('a member subscribes to her lessons, downloads them and revokes the link', async ({
  page,
}) => {
  const name = await signIn(page, 'Member');
  const nav = page.getByRole('navigation', { name: 'Main' });

  await nav.getByRole('link', { name: 'My lessons' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'My lessons' })).toBeVisible();
  // A coming lesson of the family, if the demo has one: the feed lists it.
  const region = page.getByRole('region', { name: 'Coming up' });
  const upcoming = region.getByRole('button', { name: /\d{2}:\d{2}–\d{2}:\d{2}/ }).first();
  await expect(upcoming.or(region.getByText(/^No lessons/)).first()).toBeVisible();
  const courseType =
    (await upcoming.count()) > 0
      ? ((await upcoming.getAttribute('aria-label'))?.split(', ')[0] ?? '')
      : '';

  await page.getByRole('main').getByRole('button', { name: 'Subscribe' }).click();
  const dialog = page.getByRole('dialog', { name: 'Your lessons in your calendar' });
  await expect(dialog.getByText('You have no link for this calendar yet.')).toBeVisible();
  const { filename, content } = await createAndDownload(page, dialog);

  expect(filename).toMatch(/\.ics$/);
  expect(content.startsWith('BEGIN:VCALENDAR')).toBe(true);
  expect(content).toContain('END:VCALENDAR');
  if (courseType) expect(content).toContain(courseType);

  await dialog.getByRole('button', { name: 'Close' }).click();
  await expect(dialog).toBeHidden();

  // The account settings list it, and revoke it there.
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Account settings' }).click();
  const card = page.getByRole('region', { name: 'Calendar feeds' });
  const item = card.getByRole('listitem').filter({ hasText: 'Your lessons' });
  await expect(item).toHaveCount(1);
  await expect(item.getByText(/^Created on /)).toBeVisible();
  await item.getByRole('button', { name: 'Revoke the link of Your lessons' }).click();
  await item.getByRole('button', { name: 'Revoke', exact: true }).click();
  await expect(page.getByText('The link has been revoked.')).toBeVisible();
  await expect(card.getByText(/You have no calendar links/)).toBeVisible();
  // Nothing is wider than the page, on a phone too.
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true);
});

test('the instructor subscribes to her teaching', async ({ page }) => {
  await signIn(page, 'Instructor');
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Teaching' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Teaching' })).toBeVisible();

  await page.getByRole('main').getByRole('button', { name: 'Subscribe' }).click();
  const dialog = page.getByRole('dialog', { name: 'Your teaching in your calendar' });
  const { filename, content } = await createAndDownload(page, dialog);

  expect(filename).toMatch(/\.ics$/);
  expect(content.startsWith('BEGIN:VCALENDAR')).toBe(true);
  // Her weekly ballet classes (see teach.spec.ts).
  expect(content).toContain('Classical Ballet');
  expect(content).toContain('BEGIN:VEVENT');
});

test('visitors are asked to sign in to subscribe to the schedule', async ({ page }) => {
  await gotoDemo(page, '/schedule');

  await page.getByRole('main').getByRole('button', { name: 'Subscribe' }).click();
  const dialog = page.getByRole('dialog', { name: 'The schedule in your calendar' });
  await expect(dialog.getByText('Sign in to subscribe')).toBeVisible();
  await dialog.getByRole('link', { name: 'Sign in' }).click();
  await expect(page.getByRole('region', { name: 'Demo accounts' })).toBeVisible();
});
