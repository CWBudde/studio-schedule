import { expect, type Locator, type Page, test as base } from '@playwright/test';

/**
 * The Playwright test with one extra rule: any console error or uncaught
 * exception on the page fails the test.
 */
export const test = base.extend<{ failOnPageErrors: void }>({
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
