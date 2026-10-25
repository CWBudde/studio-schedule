import { expect, type Page, test as base } from '@playwright/test';

/**
 * The Playwright test of the server smoke suite: any uncaught exception on
 * the page, or a console error other than the browser's note on a failed
 * request (the API answers 401 before sign-in, and a 4xx is part of some
 * flows), fails the test. The check wraps the `page` fixture, so it applies
 * to every test that uses a page, and a test that only sends requests
 * (`request`) opens no browser page for it.
 */
export const test = base.extend({
  page: async ({ page }, use) => {
    const errors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() === 'error' && !msg.text().startsWith('Failed to load resource')) {
        errors.push(`console: ${msg.text()}`);
      }
    });
    page.on('pageerror', (err) => errors.push(`page: ${err.message}`));
    await use(page);
    expect(errors, 'errors on the page').toEqual([]);
  },
});

export { expect, type Page };

/** The password of every seeded account (`studio-schedule seed`). */
export const seedPassword = 'demo-password';

/**
 * Seeded accounts (internal/seed), each used by one spec only, so the specs
 * can run in parallel and again on the same database.
 */
export const accounts = {
  /** A member of two weekly classes, without family. */
  member: { email: 'lina.mistlewood@example.com', name: 'Lina Mistlewood' },
  /** The office. */
  office: { email: 'olga.ledgerby@example.net', name: 'Olga Ledgerby' },
  /** A member without children, whose password the reset spec changes. */
  reset: { email: 'irene.inkwell@example.com', name: 'Irene Inkwell' },
} as const;

/** Opens the sign-in page and signs in with the form. */
export async function signIn(
  page: Page,
  account: { email: string; name: string },
  password = seedPassword,
): Promise<void> {
  await page.goto('/login');
  await expect(page.getByRole('heading', { level: 1, name: 'Sign in' })).toBeVisible();
  await page.getByLabel('Email').fill(account.email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('banner').getByRole('button', { name: account.name })).toBeVisible();
}

/** Mailpit's web API (https://mailpit.axllent.org/docs/api-v1/). */
const mailpit = (process.env.MAILPIT_URL ?? 'http://localhost:58025').replace(/\/$/, '');

type MailpitSummary = { ID: string; Created: string; Subject: string };

/**
 * Waits for the newest mail to `to` that arrived after `since` and returns
 * its plain text. The server sends mails from its outbox once a minute.
 */
export async function waitForMail(to: string, since: Date): Promise<string> {
  let id = '';
  await expect
    .poll(
      async () => {
        const res = await fetch(
          `${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`,
        );
        expect(res.ok, `Mailpit at ${mailpit} answers`).toBe(true);
        const { messages } = (await res.json()) as { messages: MailpitSummary[] };
        id = messages.find((m) => new Date(m.Created) >= since)?.ID ?? '';
        return id;
      },
      { message: `a mail to ${to}`, timeout: 120_000, intervals: [2_000] },
    )
    .not.toBe('');
  const res = await fetch(`${mailpit}/api/v1/message/${id}`);
  expect(res.ok).toBe(true);
  return ((await res.json()) as { Text: string }).Text;
}
