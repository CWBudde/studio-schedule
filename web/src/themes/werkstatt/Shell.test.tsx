import { fireEvent, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CalendarDays, Shapes } from 'lucide-react';
import { describe, expect, it, vi } from 'vitest';
import { renderInRouter } from '@/test/utils';
import type { ShellProps, ShellSession } from '../slots';
import { Shell } from './Shell';

const nav: ShellProps['header']['nav'] = [
  { to: '/schedule', label: 'Schedule', icon: CalendarDays },
  { to: '/classes', label: 'Classes', icon: Shapes },
];

/** Werkstatt's Shell around a page, with a stand-in account menu. */
function renderShell(header: Partial<ShellProps['header']> = {}) {
  return renderInRouter(
    <Shell
      banners={<p>A banner</p>}
      announcements={<p>An announcement</p>}
      header={{
        schoolName: 'Tanzwerk Nord',
        nav,
        tools: null,
        account: <button type="button">Ada Lovelace</button>,
        language: null,
        session: { state: 'signedOut' },
        ...header,
      }}
      footer={<footer>The footer</footer>}
    >
      <p>The page</p>
    </Shell>,
  );
}

const signedIn = (over: Partial<Extract<ShellSession, { state: 'signedIn' }>> = {}) =>
  ({ state: 'signedIn', signOut: vi.fn(), signingOut: false, ...over }) as const;

/** The phone menu's button and, while open, its drop panel. */
function phoneMenu() {
  const button = screen.getByRole('button', { name: 'Menu' });
  const panel = () => document.getElementById(button.getAttribute('aria-controls') ?? '');
  return { button, panel };
}

describe('Werkstatt Shell', () => {
  it('links to the nav items in the main navigation', async () => {
    renderShell();

    const tabs = within(await screen.findByRole('navigation', { name: 'Main' }));
    expect(tabs.getByRole('link', { name: 'Schedule' })).toHaveAttribute('href', '/schedule');
    expect(tabs.getByRole('link', { name: 'Classes' })).toHaveAttribute('href', '/classes');
    expect(screen.getByText('The page').closest('main')).toHaveAttribute('id', 'main');
  });

  it('offers signing in, and no account menu, when signed out', async () => {
    renderShell({ session: { state: 'signedOut' } });

    const banner = within(await screen.findByRole('banner'));
    expect(banner.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login');
    expect(screen.queryByRole('button', { name: 'Ada Lovelace' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
  });

  it('offers neither signing in nor out while the session is unknown', async () => {
    renderShell({ session: { state: 'unknown' } });

    await screen.findByRole('banner');
    expect(screen.queryByRole('link', { name: 'Sign in' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Ada Lovelace' })).not.toBeInTheDocument();
  });

  it('shows the account menu and signs out when signed in', async () => {
    const session = signedIn();
    renderShell({ session });

    const banner = within(await screen.findByRole('banner'));
    expect(banner.getByRole('button', { name: 'Ada Lovelace' })).toBeInTheDocument();
    expect(banner.queryByRole('link', { name: 'Sign in' })).not.toBeInTheDocument();

    await userEvent.click(banner.getByRole('button', { name: 'Sign out' }));

    expect(session.signOut).toHaveBeenCalledOnce();
  });

  it('disables signing out while it runs', async () => {
    renderShell({ session: signedIn({ signingOut: true }) });

    expect(await screen.findByRole('button', { name: 'Sign out' })).toBeDisabled();
  });

  it('opens the phone menu, moves the focus into it and closes it with Escape', async () => {
    const user = userEvent.setup();
    renderShell();
    await screen.findByRole('banner');
    const { button, panel } = phoneMenu();
    expect(button).toHaveAttribute('aria-expanded', 'false');
    expect(panel()).toBeNull();

    await user.click(button);

    expect(button).toHaveAttribute('aria-expanded', 'true');
    const menu = within(panel()!).getByRole('navigation', { name: 'Main' });
    expect(within(panel()!).getAllByRole('navigation')).toHaveLength(1);
    expect(within(menu).getByRole('link', { name: 'Schedule' })).toHaveFocus();
    expect(within(menu).getByRole('link', { name: 'Classes' })).toHaveAttribute('href', '/classes');
    expect(screen.getByText('The page').closest('[inert]')).not.toBeNull();

    await user.keyboard('{Escape}');

    expect(button).toHaveAttribute('aria-expanded', 'false');
    expect(panel()).toBeNull();
    expect(button).toHaveFocus();
    expect(screen.getByText('The page').closest('[inert]')).toBeNull();
  });

  it('closes the phone menu on a pointer outside it', async () => {
    const user = userEvent.setup();
    renderShell();
    await screen.findByRole('banner');
    const { button, panel } = phoneMenu();
    await user.click(button);
    expect(panel()).not.toBeNull();

    fireEvent.pointerDown(screen.getByText('The page'));

    expect(panel()).toBeNull();
    expect(button).toHaveAttribute('aria-expanded', 'false');
  });

  it('keeps the phone menu open on a pointer inside it', async () => {
    const user = userEvent.setup();
    renderShell();
    await screen.findByRole('banner');
    const { button, panel } = phoneMenu();
    await user.click(button);

    fireEvent.pointerDown(panel()!);

    expect(panel()).not.toBeNull();
  });

  it('closes the phone menu when a link in it is followed', async () => {
    const user = userEvent.setup();
    const { router } = renderShell();
    await screen.findByRole('banner');
    const { button, panel } = phoneMenu();
    await user.click(button);

    await user.click(within(panel()!).getByRole('link', { name: 'Schedule' }));

    expect(panel()).toBeNull();
    expect(router.state.location.pathname).toBe('/schedule');
  });

  it('shows no image of its own without a logo', async () => {
    const { container } = renderShell({ logoUrl: undefined });
    await screen.findByRole('banner');

    // The school's background lives in werkstatt.css (--school-background), never in the Shell.
    expect(container.querySelector('img')).toBeNull();
    for (const el of container.querySelectorAll<HTMLElement>('[style]')) {
      expect(el.style.backgroundImage).toBe('');
    }
  });

  it("names the home link after the school and shows the school's logo", async () => {
    const { container } = renderShell({ logoUrl: 'data:image/png;base64,AAAA' });

    const home = await screen.findByRole('link', { name: 'Tanzwerk Nord – home' });
    expect(home).toHaveAttribute('href', '/');
    expect(container.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,AAAA');
    expect(within(home).getByText('Tanzwerk Nord')).toBeInTheDocument();
  });

  it("names the home link after the app while the school's name is unknown", async () => {
    renderShell({ schoolName: undefined });

    const home = await screen.findByRole('link', { name: 'Studio Schedule – home' });
    expect(within(home).getByText('Studio Schedule')).toBeInTheDocument();
  });
});
