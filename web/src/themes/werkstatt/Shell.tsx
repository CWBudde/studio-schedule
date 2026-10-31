import { Link } from '@tanstack/react-router';
import { Menu, X } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import type { NavItem, ShellProps } from '../slots';

/** The width from which the tab row shows, and below which the phone menu does. */
const WIDE = '(min-width: 600px)';

/**
 * Werkstatt's app: a translucent header with the tabs below it, and the page
 * as one panel over the school's background (werkstatt.css). On phones the
 * tabs move into a drop panel, and the page behind it fades while it is open.
 */
export function Shell({ children, banners, announcements, header, footer }: ShellProps) {
  const { t } = useTranslation();
  const [menuOpen, setMenuOpen] = useState(false);
  const menuButton = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const panelId = useId();

  useEffect(() => {
    if (!menuOpen) return;
    panel.current?.querySelector('a')?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      setMenuOpen(false);
      menuButton.current?.focus();
    };
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (panel.current?.contains(target) || menuButton.current?.contains(target)) return;
      setMenuOpen(false);
    };
    // Grown past the phone width, the panel hides; the page behind must not stay faded.
    const wide = window.matchMedia(WIDE);
    const onWide = (event: MediaQueryListEvent) => {
      if (event.matches) setMenuOpen(false);
    };
    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('pointerdown', onPointerDown);
    wide.addEventListener('change', onWide);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      document.removeEventListener('pointerdown', onPointerDown);
      wide.removeEventListener('change', onWide);
    };
  }, [menuOpen]);

  return (
    <div className="flex min-h-svh flex-col">
      <a
        href="#main"
        className="sr-only z-50 rounded-[3px] bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:top-2 focus:left-2"
      >
        {t('a11y.skipToContent')}
      </a>
      {banners}
      {/* Above the page: the drop panel must cover it, and the faded page is a stacking context. */}
      <header className="relative z-30 border-b bg-(--werkstatt-header) backdrop-blur">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-2 min-[600px]:px-4">
          <Button
            ref={menuButton}
            size="icon-lg"
            className="rounded-[3px] min-[600px]:hidden"
            aria-label={t('nav.menu')}
            aria-expanded={menuOpen}
            aria-controls={panelId}
            onClick={() => setMenuOpen((open) => !open)}
          >
            {menuOpen ? <X aria-hidden="true" /> : <Menu aria-hidden="true" />}
          </Button>
          <HomeLink schoolName={header.schoolName} logoUrl={header.logoUrl} />
          <div className="flex min-w-0 items-center gap-1">
            {header.tools}
            {header.language}
            <Session {...header} />
          </div>
        </div>
        <nav
          aria-label={t('nav.label')}
          className="hidden border-t border-border/60 px-2 min-[600px]:flex min-[600px]:flex-wrap"
        >
          {header.nav.map((item) => (
            <Tab key={item.to} item={item} />
          ))}
        </nav>
        {menuOpen && (
          <div
            ref={panel}
            id={panelId}
            className="absolute inset-x-0 top-full bg-(--werkstatt-menu) shadow-lg min-[600px]:hidden"
          >
            <nav aria-label={t('nav.label')} className="flex flex-col py-2">
              {header.nav.map((item) => (
                <Link
                  key={item.to}
                  to={item.to}
                  onClick={() => setMenuOpen(false)}
                  className={cn(focusRing, menuLink)}
                >
                  <item.icon aria-hidden="true" className="size-5" />
                  {item.label}
                </Link>
              ))}
            </nav>
          </div>
        )}
      </header>
      {/* The page behind the open phone menu: faded, and out of reach of focus and pointer. */}
      <div
        inert={menuOpen}
        className={cn(
          'flex flex-1 flex-col transition-[opacity,filter] duration-200 motion-reduce:transition-none',
          menuOpen && 'opacity-20 blur-[2px]',
        )}
      >
        {announcements}
        <main
          id="main"
          tabIndex={-1}
          className="flex-1 outline-none min-[600px]:px-4 min-[600px]:py-6"
        >
          <div className="mx-auto w-full max-w-[1000px] bg-(--werkstatt-panel) p-6 min-[600px]:rounded-[3px] min-[1280px]:max-w-[1200px]">
            {children}
          </div>
        </main>
        {footer}
      </div>
    </div>
  );
}

const focusRing = 'focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none';

// The tabs: uppercase with their icon; the current page in the primary color, underlined.
const tab =
  'inline-flex items-center gap-2 border-b-2 border-transparent px-3 py-2.5 text-sm tracking-wide text-(--werkstatt-tab) uppercase transition-colors hover:text-primary data-[status=active]:border-primary data-[status=active]:text-primary motion-reduce:transition-none';

const menuLink =
  'flex items-center gap-3 px-5 py-3 text-sm tracking-wide text-(--werkstatt-tab) uppercase hover:bg-muted hover:text-primary data-[status=active]:text-primary';

function Tab({ item }: { item: NavItem }) {
  return (
    <Link to={item.to} className={cn(focusRing, tab, 'rounded-t-[3px]')}>
      <item.icon aria-hidden="true" className="size-4" />
      {item.label}
    </Link>
  );
}

/** The school's logo and name, linking home. */
function HomeLink({ schoolName, logoUrl }: Pick<ShellProps['header'], 'schoolName' | 'logoUrl'>) {
  const { t } = useTranslation();
  const { t: tSettings } = useTranslation('settings');
  return (
    <Link
      to="/"
      aria-label={schoolName ? tSettings('header.home', { school: schoolName }) : t('app.home')}
      className={cn(
        focusRing,
        'mr-auto flex min-h-10 min-w-0 items-center gap-2.5 rounded-[3px] text-lg font-bold tracking-wide',
      )}
    >
      {logoUrl && <img src={logoUrl} alt="" className="h-9 w-auto max-w-32 object-contain" />}
      <span className="truncate">{schoolName ?? t('app.name')}</span>
    </Link>
  );
}

// The solid primary button in small caps; the » before its label is decoration.
const sessionButton = 'rounded-[3px] px-3 tracking-wide [font-variant-caps:small-caps]';

/**
 * "» Sign in" when signed out; the account menu and "» Sign out" when signed
 * in; nothing while the session is unknown, so neither flashes.
 */
function Session({ session, account }: Pick<ShellProps['header'], 'session' | 'account'>) {
  const { t } = useTranslation('auth');
  if (session.state === 'unknown') return null;
  if (session.state === 'signedOut') {
    return (
      <Button asChild className={sessionButton}>
        <Link to="/login">
          <span aria-hidden="true">»</span>
          {t('account.signIn')}
        </Link>
      </Button>
    );
  }
  return (
    <>
      {account}
      <Button
        className={sessionButton}
        disabled={session.signingOut}
        onClick={() => session.signOut()}
      >
        <span aria-hidden="true">»</span>
        {t('account.signOut')}
      </Button>
    </>
  );
}
