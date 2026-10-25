import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LanguageSwitcher } from '@/components/layout/LanguageSwitcher';
import { HomePage } from '@/features/home/HomePage';
import { healthOk, mockApiGet, publicSettings, schedule } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import i18n, { supportedLanguage } from './i18n';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('i18n', () => {
  it('defaults to English', () => {
    expect(i18n.resolvedLanguage).toBe('en');
  });

  it('switches the UI to German via the language switcher', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
    });
    const user = userEvent.setup();

    renderInRouter(
      <>
        <LanguageSwitcher />
        <HomePage />
      </>,
    );
    expect(await screen.findByRole('heading', { name: 'Next lessons' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Change language' }));
    await user.click(await screen.findByRole('menuitemradio', { name: 'Deutsch' }));

    expect(await screen.findByRole('heading', { name: 'Nächste Stunden' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Systemstatus' })).toBeInTheDocument();
    expect(i18n.resolvedLanguage).toBe('de');
    expect(document.documentElement.lang).toBe('de');
    // A choice of the visitor's own: it outranks the school's language.
    expect(localStorage.getItem('studio-schedule.lang')).toBe('de');
  });

  it('matches language tags to the supported languages', () => {
    expect(supportedLanguage('de-AT')).toBe('de');
    expect(supportedLanguage('en_GB')).toBe('en');
    expect(supportedLanguage('fr')).toBeUndefined();
    expect(supportedLanguage(undefined)).toBeUndefined();
  });

  it.each([
    'common',
    'schedule',
    'auth',
    'account',
    'timetable',
    'admin',
    'teach',
    'myLessons',
    'feeds',
    'passes',
    'receipts',
    'settings',
  ])('has the same %s keys in all locales', async (ns) => {
    const [en, de] = await Promise.all([
      import(`./locales/en/${ns}.json`) as Promise<{ default: object }>,
      import(`./locales/de/${ns}.json`) as Promise<{ default: object }>,
    ]);
    const keys = (obj: object, prefix = ''): string[] =>
      Object.entries(obj).flatMap(([k, v]) =>
        typeof v === 'object' && v !== null
          ? keys(v as object, `${prefix}${k}.`)
          : [`${prefix}${k}`],
      );
    expect(keys(de.default).sort()).toEqual(keys(en.default).sort());
  });
});
