import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { DEMO_SCHOOL_KEY, switchDemoSchool } from '@/features/demo/school';
import i18n from '@/i18n';
import { renderInRouter } from '@/test/utils';
import { DemoBanner } from './DemoBanner';

vi.mock('@/lib/env', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/env')>()),
  transport: 'wasm',
  isDemo: true,
}));

// Switching schools reloads the page, which jsdom cannot do.
vi.mock('@/features/demo/school', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/demo/school')>()),
  switchDemoSchool: vi.fn(),
}));

afterEach(() => {
  localStorage.removeItem(DEMO_SCHOOL_KEY);
  vi.mocked(switchDemoSchool).mockClear();
});

describe('DemoBanner', () => {
  it('leads to the about page', async () => {
    renderInRouter(<DemoBanner />);

    const banner = await screen.findByRole('complementary', { name: 'Demo mode' });
    expect(within(banner).getByRole('link', { name: 'About this demo' })).toHaveAttribute(
      'href',
      '/about',
    );
  });

  it('switches from the programme school to the Werkstatt school', async () => {
    const user = userEvent.setup();
    renderInRouter(<DemoBanner />);

    const banner = await screen.findByRole('complementary', { name: 'Demo mode' });
    await user.click(within(banner).getByRole('button', { name: 'Switch to the Werkstatt look' }));

    expect(switchDemoSchool).toHaveBeenCalledExactlyOnceWith('werkstatt');
  });

  it('switches from the Werkstatt school back to the programme school', async () => {
    localStorage.setItem(DEMO_SCHOOL_KEY, 'werkstatt');
    const user = userEvent.setup();
    renderInRouter(<DemoBanner />);

    const banner = await screen.findByRole('complementary', { name: 'Demo mode' });
    expect(
      within(banner).queryByRole('button', { name: 'Switch to the Werkstatt look' }),
    ).not.toBeInTheDocument();
    await user.click(within(banner).getByRole('button', { name: 'Switch to the Programme look' }));

    expect(switchDemoSchool).toHaveBeenCalledExactlyOnceWith('programme');
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    try {
      renderInRouter(<DemoBanner />);

      const banner = await screen.findByRole('complementary', { name: 'Demo-Modus' });
      expect(
        within(banner).getByRole('button', { name: 'Zur Werkstatt-Optik wechseln' }),
      ).toBeInTheDocument();
    } finally {
      await i18n.changeLanguage('en');
    }
  });
});
