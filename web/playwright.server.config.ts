import { defineConfig, devices } from '@playwright/test';

// Smoke tests against the production server (`bin/studio-schedule serve`,
// built by `just build`) on PostgreSQL, with mails caught by Mailpit: real
// cookies, the cross-origin protection and PostgreSQL's semantics, which the
// WASM demo of `playwright.config.ts` does not have. `just e2e-server`
// (scripts/e2e-server.sh) starts PostgreSQL and Mailpit, migrates and seeds
// the database and runs this suite; CI passes its service containers to that
// script in E2E_DATABASE_URL, MAILPIT_URL (Mailpit's web API) and
// MAILPIT_SMTP (host:port). The script hands the database to this config as
// DATABASE_URL.
const port = Number(process.env.E2E_SERVER_PORT ?? 8090);
const baseURL = `http://localhost:${port}`;
const ci = !!process.env.CI;

const databaseURL = process.env.DATABASE_URL;
if (!databaseURL) throw new Error('DATABASE_URL is not set: run `just e2e-server`');
const [smtpHost, smtpPort] = (process.env.MAILPIT_SMTP ?? 'localhost:51025').split(':');

export default defineConfig({
  testDir: 'e2e-server',
  forbidOnly: ci,
  // The tests are written to pass when run again on the same database.
  retries: ci ? 1 : 0,
  reporter: ci ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    timezoneId: 'America/New_York',
    locale: 'en-US',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'server-smoke',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } },
    },
  ],
  webServer: {
    command: '../bin/studio-schedule serve',
    env: {
      DATABASE_URL: databaseURL,
      HTTP_ADDR: `:${port}`,
      BASE_URL: baseURL,
      SMTP_HOST: smtpHost ?? 'localhost',
      SMTP_PORT: smtpPort ?? '51025',
      SMTP_TLS: 'none',
      SMTP_FROM: 'Studio Schedule <schedule@example.test>',
      // Warnings and errors show in the test output.
      LOG_LEVEL: process.env.LOG_LEVEL ?? 'warn',
    },
    url: `${baseURL}/readyz`,
    // A server left running would be one without this run's database.
    reuseExistingServer: false,
    stderr: 'pipe',
    timeout: 60_000,
  },
});
