import { screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { healthOk, mockApiGet, publicSettings, signedOut } from '@/test/mockApi';
import i18n from '@/i18n';
import { renderInRouter } from '@/test/utils';
import { AppFooter } from './AppFooter';
import { AppHeader } from './AppHeader';
import { accentStyleSheet } from './branding';

afterEach(() => {
  vi.restoreAllMocks();
  document.getElementById('school-accent')?.remove();
  document.title = 'Studio Schedule';
});

const notDemo = { '/api/v1/demo/mailbox': { status: 404, body: undefined } };
const branded = {
  ...publicSettings,
  schoolName: 'Sample Ballet Academy',
  accentColor: '#1d4ed8',
  accentColorDark: '#97b7f8',
  hasLogo: true,
  legalNotice: 'Sample Ballet Academy\n1 Example Street, Sampletown',
};

describe('School branding', () => {
  it('names the school, shows its logo and applies its accent', async () => {
    mockApiGet({
      ...signedOut,
      ...notDemo,
      '/api/v1/settings/public': { body: branded },
      '/api/v1/settings/logo': { body: new Blob(['png'], { type: 'image/png' }) },
    });

    renderInRouter(<AppHeader />);

    const home = await screen.findByRole('link', { name: 'Sample Ballet Academy – home' });
    expect(home).toHaveTextContent('Sample Ballet Academy');
    await waitFor(() =>
      expect(home.querySelector('img')).toHaveAttribute(
        'src',
        `data:image/png;base64,${btoa('png')}`,
      ),
    );
    expect(document.title).toBe('Sample Ballet Academy');
    expect(document.getElementById('school-accent')).toHaveTextContent(
      accentStyleSheet('#1d4ed8', '#97b7f8'),
    );
  });

  it("keeps the app's mark and colors without a logo or accent", async () => {
    mockApiGet({
      ...signedOut,
      ...notDemo,
      '/api/v1/settings/public': { body: { ...publicSettings, hasLogo: false } },
    });

    renderInRouter(<AppHeader />);

    const home = await screen.findByRole('link', { name: 'Example Dance Studio – home' });
    expect(home.querySelector('img')).toBeNull();
    expect(home.querySelector('svg')).not.toBeNull();
    expect(document.getElementById('school-accent')).toBeNull();
    await waitFor(() => expect(document.title).toBe('Example Dance Studio'));
  });

  it("names the page after the app, in the UI's language, until the settings load", async () => {
    mockApiGet({
      ...signedOut,
      ...notDemo,
      '/api/v1/settings/public': { status: 500, body: undefined },
    });
    document.title = '';

    renderInRouter(<AppHeader />);

    await waitFor(() => expect(document.title).toBe(i18n.t('app.name')));
  });

  it("follows the school's language for a visitor who chose none", async () => {
    mockApiGet({
      ...signedOut,
      ...notDemo,
      '/api/v1/settings/public': { body: { ...publicSettings, defaultLocale: 'de' } },
    });

    renderInRouter(<AppHeader />);

    expect(await screen.findByRole('link', { name: 'Stundenplan' })).toBeInTheDocument();
    expect(i18n.resolvedLanguage).toBe('de');
    expect(document.documentElement.lang).toBe('de');
    // Remembered, so the next page load starts in it.
    expect(localStorage.getItem('studio-schedule.school-lang')).toBe('de');
  });

  it("keeps the visitor's own language choice over the school's", async () => {
    localStorage.setItem('studio-schedule.lang', 'en');
    mockApiGet({
      ...signedOut,
      ...notDemo,
      '/api/v1/settings/public': { body: { ...publicSettings, defaultLocale: 'de' } },
    });

    renderInRouter(<AppHeader />);

    expect(
      await screen.findByRole('link', { name: 'Example Dance Studio – home' }),
    ).toBeInTheDocument();
    await waitFor(() => expect(localStorage.getItem('studio-schedule.school-lang')).toBe('de'));
    expect(screen.getByRole('link', { name: 'Schedule' })).toBeInTheDocument();
    expect(i18n.resolvedLanguage).toBe('en');
  });

  it('shows the legal notice in the footer', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: branded },
    });

    renderInRouter(<AppFooter />);

    expect(await screen.findByRole('region', { name: 'Legal notice' })).toHaveTextContent(
      'Sample Ballet Academy 1 Example Street, Sampletown',
    );
  });
});

describe('accentStyleSheet', () => {
  it('overrides the primary color in both themes, the dark one with its variant', () => {
    const css = accentStyleSheet('#1d4ed8', '#97b7f8');
    expect(css).toContain('html:root{--primary:#1d4ed8;');
    expect(css).toContain('html.dark{--primary:#97b7f8;');
    // Light text stays on the light variant in the dark theme.
    expect(css).toMatch(/html\.dark\{[^}]*--day-today:#1d4ed8;/);
  });
});
