import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LanguageSwitcher } from '@/components/layout/LanguageSwitcher';
import { HomePage } from '@/features/home/HomePage';
import { healthOk, mockApiGet, publicSettings } from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import i18n from './i18n';

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
    });
    const user = userEvent.setup();

    renderWithQuery(
      <>
        <LanguageSwitcher />
        <HomePage />
      </>,
    );
    expect(screen.getByText('Schedule coming in Phase 2')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Change language' }));
    await user.click(await screen.findByRole('menuitemradio', { name: 'Deutsch' }));

    expect(await screen.findByText('Stundenplan folgt in Phase 2')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Systemstatus' })).toBeInTheDocument();
    expect(i18n.resolvedLanguage).toBe('de');
    expect(document.documentElement.lang).toBe('de');
  });

  it('has the same keys in all locales', async () => {
    const [en, de] = await Promise.all([
      import('./locales/en/common.json'),
      import('./locales/de/common.json'),
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
