import { demoName, expect, gotoDemo, lessons, type Page, signIn, test } from './fixtures';

/*
 * The demo seed has a workshop for members only 5 days after the visit date
 * and a hidden one 6 days after it, and a room rental (hidden too) 4 days
 * after it, all within the 14 days the schedule list shows by default.
 */
const membersOnly = /, Members only$/;
const hidden = /, Workshop, Hidden – staff only$/;
const rental = /, Room rental, Copperleaf Theater Group, Hidden – staff only$/;

/** Opens the schedule from the main navigation and waits for its lessons. */
async function openSchedule(page: Page): Promise<void> {
  await page
    .getByRole('navigation', { name: 'Main' })
    .getByRole('link', { name: 'Schedule' })
    .click();
  await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toBeVisible();
  await expect(lessons(page.getByRole('main')).first()).toBeVisible();
}

test('an admin signs in as a member, sees what they see and returns', async ({ page }) => {
  await gotoDemo(page, '/login');
  const member = await demoName(page, 'Member');
  const admin = await signIn(page, 'Administrator');
  const header = page.getByRole('banner');

  await header.getByRole('button', { name: admin }).click();
  await page.getByRole('menuitem', { name: 'Accounts' }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Accounts' })).toBeVisible();
  const table = page.getByRole('table', { name: 'Accounts' });
  await expect(table.getByRole('button', { name: `Sign in as ${admin}` })).toBeDisabled();
  await table.getByRole('button', { name: `Sign in as ${member}` }).click();

  const banner = page.getByRole('region', { name: 'Signed in as someone else' });
  await expect(banner).toBeVisible();
  await expect(banner).toContainText(`You are signed in as ${member} (`);
  await expect(banner).toContainText(`) by ${admin}.`);
  expect(new URL(page.url()).hash).toBe('#/');
  await expect(header.getByRole('button', { name: member })).toBeVisible();

  // The member sees the workshop for members, but not the hidden one.
  await openSchedule(page);
  const main = page.getByRole('main');
  const workshop = lessons(main).filter({ hasText: 'Members only' });
  await expect(workshop).toHaveCount(1);
  await expect(workshop).toHaveAccessibleName(membersOnly);
  await expect(lessons(main).filter({ hasText: 'Hidden – staff only' })).toHaveCount(0);
  await expect(lessons(main).filter({ hasText: 'Room rental' })).toHaveCount(0);
  await expect(main.getByText(/^Early access: as a member you already see classes/)).toBeVisible();
  // The banner stays on every page.
  await expect(banner).toBeVisible();

  await banner.getByRole('button', { name: 'Return to my account' }).click();

  await expect(banner).toBeHidden();
  await expect(header.getByRole('button', { name: admin })).toBeVisible();
  // As staff again, the schedule shows the hidden workshop and the room rental as well.
  await expect(lessons(main).filter({ hasText: 'Hidden – staff only' })).toHaveCount(2);
});

test('a visitor sees neither the workshop for members nor the hidden one', async ({ page }) => {
  await gotoDemo(page, '/schedule');
  const main = page.getByRole('main');
  await expect(lessons(main).first()).toBeVisible();

  const names = await lessons(main).evaluateAll((els) =>
    els.map((el) => el.getAttribute('aria-label') ?? ''),
  );
  expect(
    names.filter((name) => membersOnly.test(name) || hidden.test(name) || rental.test(name)),
  ).toEqual([]);
  await expect(main.getByText('Members only')).toHaveCount(0);
  await expect(main.getByText('Hidden – staff only')).toHaveCount(0);
  await expect(main.getByText('Room rental')).toHaveCount(0);
});

test('an instructor sees the hidden workshop and the room rental with their badges', async ({
  page,
}) => {
  await signIn(page, 'Instructor');

  await openSchedule(page);

  const main = page.getByRole('main');
  const workshop = lessons(main).filter({ hasText: 'Workshop' }).filter({
    hasText: 'Hidden – staff only',
  });
  await expect(workshop).toHaveCount(1);
  await expect(workshop).toHaveAccessibleName(hidden);
  await expect(workshop.getByText('Hidden – staff only')).toBeVisible();

  // The rental names its renter instead of an instructor and offers no booking.
  const rented = lessons(main).filter({ hasText: 'Room rental' });
  await expect(rented).toHaveCount(1);
  await expect(rented).toHaveAccessibleName(rental);
  await expect(rented.getByText('Copperleaf Theater Group')).toBeVisible();
  await expect(main.getByText(/^As staff you see every lesson/)).toBeVisible();
});
