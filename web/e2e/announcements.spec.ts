import { expect, gotoDemo, type Page, signIn, signOut, test } from './fixtures';

// The demo seed has two announcements of the office: one for everyone, and
// one for the members of the Creative Dance classes, which the member login
// (Clara Amberly) sees through her son. Neither was emailed. Beside them is
// a draft for everyone, "Open day in spring", which nobody sees until the
// office publishes it.

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

/** Writes an announcement for everyone as the office, optionally emailed. */
async function announce(page: Page, title: string, text: string, email: boolean) {
  await page.getByRole('button', { name: 'New announcement' }).click();
  const sheet = page.getByRole('dialog', { name: 'New announcement' });
  await sheet.getByLabel('Title').fill(title);
  await sheet.getByLabel('Text').fill(text);
  if (email) await sheet.getByRole('switch', { name: 'Also send by email' }).click();
  await sheet.getByRole('button', { name: 'Publish' }).click();
  if (email) {
    const confirm = page.getByRole('alertdialog', { name: 'Send this announcement by email?' });
    await expect(confirm.getByText(/^It goes to \d+ addresses now/)).toBeVisible();
    await confirm.getByRole('button', { name: 'Publish and send' }).click();
  }
  // With email, the announcements job sends it right after it is published:
  // the toast says it is queued.
  await expect(
    page.getByText(
      email ? `“${title}” is published; its email is queued.` : `“${title}” is published.`,
    ),
  ).toBeVisible();
}

test('an announcement of the office shows as a banner, is dismissed, and stays on the home page', async ({
  page,
}) => {
  const office = await signIn(page, 'Office');
  await openAdmin(page, office, 'Announcements');
  await announce(
    page,
    'Studio closed for painting',
    'Closed next Monday.\nSee you on Tuesday.',
    false,
  );
  await signOut(page, office);

  await signIn(page, 'Member');
  const banner = page.getByRole('region', { name: 'New announcements' });
  // The newest first, then the seed's two.
  await expect(banner).toContainText('Studio closed for painting');
  await expect(banner).toContainText('and 2 more');
  await banner.getByRole('button', { name: 'Show the 3 announcements' }).click();
  await expect(banner.getByText('Costume fitting for the winter show')).toBeVisible();
  await expect(banner.getByText(/See you on Tuesday\./)).toBeVisible();
  await banner.getByRole('button', { name: 'Dismiss “Studio closed for painting”' }).click();
  await expect(banner.getByText('Studio closed for painting')).toHaveCount(0);
  await expect(banner.getByText('Costume fitting for the winter show')).toBeVisible();

  // The banner is on every page; the home page lists the dismissed one too.
  await gotoDemo(page, '/');
  const list = page.getByRole('region', { name: 'Announcements', exact: true });
  const dismissed = list.getByRole('listitem').filter({ hasText: 'Studio closed for painting' });
  await expect(dismissed).toContainText('Dismissed');
  await expect(dismissed).toContainText('Closed next Monday.');
  await expect(page.getByRole('region', { name: 'New announcements' })).not.toContainText(
    'Studio closed for painting',
  );
});

test('the mail log lists the mails of an emailed announcement', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openAdmin(page, office, 'Announcements');
  await announce(page, 'Rehearsal schedule', 'The rehearsals start next week.', true);
  // The office does not see the mail log.
  await expect(
    page
      .getByRole('navigation', { name: 'Administration' })
      .getByRole('link', { name: 'Mail log' }),
  ).toHaveCount(0);
  await signOut(page, office);

  const admin = await signIn(page, 'Administrator');
  await openAdmin(page, admin, 'Mail log');
  await expect(
    page.getByText(/^The demo delivers no mail: every mail stays pending/),
  ).toBeVisible();
  const mails = page.getByRole('table', { name: 'Mails' });
  const rehearsal = mails.getByRole('row').filter({ hasText: 'Rehearsal schedule' });
  await expect(rehearsal.first()).toContainText('Pending');
  await expect(rehearsal.first()).toContainText('Announcement');

  await rehearsal
    .first()
    .getByRole('button', { name: /^Show the mail to / })
    .click();
  const sheet = page.getByRole('dialog', { name: /^Mail to / });
  await expect(sheet.getByText(/The rehearsals start next week\./)).toBeVisible();
});

test('a draft of the office is shown to nobody until the office publishes it', async ({ page }) => {
  const office = await signIn(page, 'Office');
  await openAdmin(page, office, 'Announcements');
  // A table on the desktop, a list of cards on a phone.
  const draft = page
    .getByRole('table', { name: 'Announcements' })
    .getByRole('row')
    .or(page.getByRole('list', { name: 'Announcements' }).getByRole('listitem'))
    .filter({ hasText: 'Open day in spring' });
  await expect(draft).toContainText('Draft');
  await expect(draft).toContainText('Not yet');
  await signOut(page, office);

  const member = await signIn(page, 'Member');
  const banner = page.getByRole('region', { name: 'New announcements' });
  await expect(banner).toContainText('and 1 more');
  await expect(banner).not.toContainText('Open day in spring');
  await signOut(page, member);

  await signIn(page, 'Office');
  await openAdmin(page, office, 'Announcements');
  await draft.getByRole('button', { name: 'Edit “Open day in spring”' }).click();
  const sheet = page.getByRole('dialog', { name: 'Edit “Open day in spring”' });
  await sheet.getByRole('button', { name: 'Publish' }).click();
  await expect(page.getByText('“Open day in spring” is published.')).toBeVisible();
  // Published now: its status follows the page's clock, which ticks each
  // minute, so it is no longer a draft rather than shown already.
  await expect(draft).not.toContainText('Draft');
  await expect(draft).not.toContainText('Not yet');
  await signOut(page, office);

  await signIn(page, 'Member');
  await expect(page.getByRole('region', { name: 'New announcements' })).toContainText(
    'Open day in spring',
  );
});
