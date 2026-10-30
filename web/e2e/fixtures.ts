import { expect, type Locator, type Page, test as base } from '@playwright/test';

/**
 * The Playwright test with two extras: any console error or uncaught
 * exception on the page fails the test, and the demo opens without its about
 * page (as for a returning visitor) unless a test sets `demoEntered: false`.
 */
export const test = base.extend<{ demoEntered: boolean; failOnPageErrors: void; enterDemo: void }>({
  demoEntered: [true, { option: true }],
  enterDemo: [
    async ({ context, demoEntered }, use) => {
      // On the context, so a second tab of a test is a returning visitor too.
      if (demoEntered) {
        await context.addInitScript(() =>
          localStorage.setItem('studio-schedule-demo-entered', '1'),
        );
      }
      await use();
    },
    { auto: true },
  ],
  failOnPageErrors: [
    async ({ page }, use) => {
      const errors: string[] = [];
      page.on('console', (msg) => {
        if (msg.type() === 'error') errors.push(`console: ${msg.text()}`);
      });
      page.on('pageerror', (err) => errors.push(`page: ${err.message}`));
      await use();
      expect(errors, 'errors on the page').toEqual([]);
    },
    { auto: true },
  ],
});

export { expect, type Page };

/**
 * Opens a route of the demo (`path` is the hash route, e.g. `/schedule?view=week`,
 * optionally preceded by a real query string such as `?ref=x#`) and waits
 * until the WASM backend has started and the page (or a sheet) shows.
 */
export async function gotoDemo(page: Page, path = '/'): Promise<void> {
  await page.goto(path.includes('#') ? path : `#${path}`);
  await expect(page.getByText('Starting demo backend…')).toBeHidden({ timeout: 30_000 });
  // A linked lesson opens as a modal sheet, which hides the page heading.
  await expect(
    page.getByRole('heading', { level: 1 }).or(page.getByRole('dialog')).first(),
  ).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Demo crashed — reload' })).toHaveCount(0);
}

/** The search params of the current hash route. */
export function hashSearch(page: Page): URLSearchParams {
  const hash = new URL(page.url()).hash;
  return new URLSearchParams(hash.slice(hash.indexOf('?') + 1 || hash.length));
}

/** Lesson cards and week-grid blocks: buttons named "<course>, <day>, <hh:mm–hh:mm>…". */
export function lessons(scope: Page | Locator): Locator {
  return scope.getByRole('button', { name: /\d{2}:\d{2}–\d{2}:\d{2}/ });
}

/** Adds days to a school date (YYYY-MM-DD), on the calendar. */
export function addDays(date: string, days: number): string {
  const d = new Date(`${date}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + days);
  return d.toISOString().slice(0, 10);
}

/** The roles of the demo accounts, as the login page names them. */
export type DemoRole = 'Member' | 'Instructor' | 'Office' | 'Administrator';

/** The button of the demo account of a role on the login page. */
export function demoAccount(page: Page, role: DemoRole): Locator {
  return page
    .getByRole('region', { name: 'Demo accounts' })
    .getByRole('button', { name: new RegExp(`^Sign in as .+ \\(${role}\\)$`) });
}

/** The name of the demo account of a role, read on the login page. */
export async function demoName(page: Page, role: DemoRole): Promise<string> {
  const label = (await demoAccount(page, role).getAttribute('aria-label')) ?? '';
  const name = new RegExp(`^Sign in as (.+) \\(${role}\\)$`).exec(label)?.[1] ?? '';
  expect(name).not.toBe('');
  return name;
}

/** The email of the demo account of a role, read on the login page. */
export async function demoEmail(page: Page, role: DemoRole): Promise<string> {
  // `has` is relative to each list item: the button without its region.
  const button = page.getByRole('button', { name: new RegExp(`^Sign in as .+ \\(${role}\\)$`) });
  const row = page
    .getByRole('region', { name: 'Demo accounts' })
    .getByRole('listitem')
    .filter({ has: button });
  const email = ((await row.getByRole('definition').first().textContent()) ?? '').trim();
  expect(email).toContain('@');
  return email;
}

/**
 * Signs in with the demo account of a role and returns its name. It uses
 * the login page: the one shown, or else the one the header's "Sign in"
 * opens, or, before anything is open, the demo's.
 */
export async function signIn(page: Page, role: DemoRole): Promise<string> {
  if (!(await page.getByRole('region', { name: 'Demo accounts' }).isVisible())) {
    if (page.url() === 'about:blank') await gotoDemo(page, '/login');
    else await page.getByRole('banner').getByRole('link', { name: 'Sign in' }).click();
  }
  const name = await demoName(page, role);
  await demoAccount(page, role).click();
  await expect(page.getByRole('banner').getByRole('button', { name })).toBeVisible();
  return name;
}

/** Signs the account `name` out from the account menu in the header. */
export async function signOut(page: Page, name: string): Promise<void> {
  await page.getByRole('banner').getByRole('button', { name }).click();
  await page.getByRole('menuitem', { name: 'Sign out' }).click();
  await expect(page.getByRole('banner').getByRole('link', { name: 'Sign in' })).toBeVisible();
}
