import { defineConfig, devices } from '@playwright/test';

// End-to-end tests against the GitHub Pages demo build (`just demo-build`):
// the real WASM backend in a Web Worker, served by `vite preview`.
const base = '/studio-schedule/';
const port = 4173;
const ci = !!process.env.CI;

export default defineConfig({
  testDir: 'e2e',
  forbidOnly: ci,
  retries: ci ? 1 : 0,
  reporter: ci ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: `http://localhost:${port}${base}`,
    // Far from the school (Europe/Berlin): the browser's zone must not leak
    // into school dates and times.
    timezoneId: 'America/New_York',
    locale: 'en-US',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'desktop',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } },
    },
    { name: 'mobile', use: { ...devices['Pixel 7'] } },
  ],
  webServer: {
    command: `pnpm preview --mode demo --port ${port} --strictPort`,
    env: { VITE_BASE: base },
    url: `http://localhost:${port}${base}`,
    reuseExistingServer: !ci,
  },
});
