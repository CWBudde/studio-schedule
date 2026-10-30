import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import i18n from '@/i18n';
import { renderInRouter } from '@/test/utils';
import { AboutPage } from './AboutPage';
import { hasEnteredDemo } from './entered';

afterEach(() => {
  localStorage.removeItem('studio-schedule-demo-entered');
  vi.restoreAllMocks();
});

describe('AboutPage', () => {
  it('explains the demo without calling the API', async () => {
    const get = vi.spyOn(api, 'GET');

    renderInRouter(<AboutPage />);

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Studio Schedule' }),
    ).toBeInTheDocument();
    for (const name of ['What it does', 'How the demo works', 'Things to try', 'Learn more']) {
      expect(screen.getByRole('heading', { level: 2, name })).toBeInTheDocument();
    }
    expect(screen.getByText('demo-password')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Sign in with a demo account' })).toHaveAttribute(
      'href',
      '/login',
    );
    expect(screen.getByRole('link', { name: /How the demo is built/ })).toHaveAttribute(
      'href',
      'https://github.com/CWBudde/studio-schedule/blob/main/docs/demo-mode.md',
    );
    expect(get).not.toHaveBeenCalled();
  });

  it('remembers that the visitor opened the demo', async () => {
    const user = userEvent.setup();
    const { router } = renderInRouter(<AboutPage />, { at: '/login' });

    const hero = await screen.findByRole('region', { name: 'Studio Schedule' });
    expect(hasEnteredDemo()).toBe(false);
    await user.click(within(hero).getByRole('link', { name: 'Open the demo' }));

    expect(hasEnteredDemo()).toBe(true);
    expect(router.state.location.pathname).toBe('/');
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    try {
      renderInRouter(<AboutPage />);

      expect(
        await screen.findByRole('heading', { level: 2, name: 'So funktioniert die Demo' }),
      ).toBeInTheDocument();
      expect(screen.getAllByRole('link', { name: 'Demo öffnen' })).toHaveLength(2);
    } finally {
      await i18n.changeLanguage('en');
    }
  });
});
