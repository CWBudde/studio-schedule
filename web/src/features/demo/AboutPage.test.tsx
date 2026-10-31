import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import i18n from '@/i18n';
import { renderInRouter } from '@/test/utils';
import { AboutPage } from './AboutPage';
import { hasEnteredDemo } from './entered';
import { DEMO_SCHOOL_KEY, switchDemoSchool } from './school';

// Switching schools reloads the page, which jsdom cannot do.
vi.mock('./school', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./school')>()),
  switchDemoSchool: vi.fn(),
}));

afterEach(() => {
  localStorage.removeItem('studio-schedule-demo-entered');
  localStorage.removeItem(DEMO_SCHOOL_KEY);
  vi.restoreAllMocks();
  vi.mocked(switchDemoSchool).mockClear();
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
    expect(screen.getByText(/two fictional schools/)).toBeInTheDocument();
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
    expect(switchDemoSchool).not.toHaveBeenCalled();
  });

  it('offers the Werkstatt school next to the demo, its preview hidden from assistive technology', async () => {
    renderInRouter(<AboutPage />);

    const hero = await screen.findByRole('region', { name: 'Studio Schedule' });
    const werkstatt = within(hero).getByRole('link', { name: 'Open in the Werkstatt look' });
    expect(werkstatt).toHaveAttribute('href', '/');
    const preview = werkstatt.querySelector('[data-slot="theme-preview"]');
    expect(preview).toHaveAttribute('aria-hidden', 'true');
    expect(preview?.textContent).toBe('');
  });

  it('switches to the Werkstatt school with a reload while another school is chosen', async () => {
    const user = userEvent.setup();
    const { router } = renderInRouter(<AboutPage />, { at: '/login' });

    const hero = await screen.findByRole('region', { name: 'Studio Schedule' });
    await user.click(within(hero).getByRole('link', { name: 'Open in the Werkstatt look' }));

    expect(switchDemoSchool).toHaveBeenCalledExactlyOnceWith('werkstatt');
    // The reload goes to the home page; the router does not navigate first.
    expect(router.state.location.pathname).toBe('/login');
  });

  it('opens the chosen school in place', async () => {
    localStorage.setItem(DEMO_SCHOOL_KEY, 'werkstatt');
    const user = userEvent.setup();
    const { router } = renderInRouter(<AboutPage />, { at: '/login' });

    const hero = await screen.findByRole('region', { name: 'Studio Schedule' });
    await user.click(within(hero).getByRole('link', { name: 'Open in the Werkstatt look' }));

    expect(switchDemoSchool).not.toHaveBeenCalled();
    expect(hasEnteredDemo()).toBe(true);
    expect(router.state.location.pathname).toBe('/');
  });

  it('opens the programme school with either "Open the demo" when Werkstatt is chosen', async () => {
    localStorage.setItem(DEMO_SCHOOL_KEY, 'werkstatt');
    const user = userEvent.setup();
    renderInRouter(<AboutPage />, { at: '/login' });

    const [hero, bottom] = await screen.findAllByRole('link', { name: 'Open the demo' });
    await user.click(hero!);
    await user.click(bottom!);

    expect(vi.mocked(switchDemoSchool).mock.calls).toEqual([['programme'], ['programme']]);
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    try {
      renderInRouter(<AboutPage />);

      expect(
        await screen.findByRole('heading', { level: 2, name: 'So funktioniert die Demo' }),
      ).toBeInTheDocument();
      expect(screen.getAllByRole('link', { name: 'Demo öffnen' })).toHaveLength(2);
      expect(
        screen.getByRole('link', { name: 'In der Werkstatt-Optik öffnen' }),
      ).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage('en');
    }
  });
});
