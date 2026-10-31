import { Link } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { BrandMark } from '@/components/layout/BrandMark';
import type { ShellProps } from '../slots';

/** Programme's app: the banners, the masthead, the page and the footer in one column. */
export function Shell({ children, banners, announcements, header, footer }: ShellProps) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-svh flex-col">
      <a
        href="#main"
        className="sr-only z-50 rounded-md bg-primary px-3 py-2 text-primary-foreground focus:not-sr-only focus:absolute focus:top-2 focus:left-2"
      >
        {t('a11y.skipToContent')}
      </a>
      {banners}
      <Header {...header} />
      {announcements}
      <main
        id="main"
        tabIndex={-1}
        className="mx-auto w-full max-w-5xl flex-1 px-4 py-8 outline-none"
      >
        {children}
      </main>
      {footer}
    </div>
  );
}

// Programme-style small capitals; the current page is underlined in the primary color.
const navLink =
  'relative rounded-sm px-2 py-1.5 text-xs font-medium tracking-[0.16em] text-muted-foreground uppercase transition-colors after:absolute after:inset-x-2 after:bottom-0 after:h-px after:origin-left after:scale-x-0 after:bg-primary after:transition-transform hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none data-[status=active]:text-primary data-[status=active]:after:scale-x-100 motion-reduce:after:transition-none sm:px-3 sm:after:inset-x-3';

/** Programme's masthead: the school, the main navigation and the tools in one row. */
export function Header({
  schoolName,
  logoUrl,
  nav,
  tools,
  account,
  language,
}: ShellProps['header']) {
  const { t } = useTranslation();
  const { t: tSettings } = useTranslation('settings');

  // Two hairlines at the bottom, like the rule under a programme's masthead.
  return (
    <header className="border-b bg-background/80 shadow-[0_3px_0_-2px_var(--border)] backdrop-blur supports-[backdrop-filter]:bg-background/70">
      {/* One row on wider screens; on phones the main navigation gets a row of its own. */}
      <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-1 px-4">
        <Link
          to="/"
          aria-label={schoolName ? tSettings('header.home', { school: schoolName }) : t('app.home')}
          className="mr-auto flex h-16 items-center gap-2.5 rounded-md font-heading text-xl font-semibold tracking-tight focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          {logoUrl ? (
            <img src={logoUrl} alt="" className="h-9 w-auto max-w-32 object-contain" />
          ) : (
            <BrandMark className="size-9" />
          )}
          <span className="hidden sm:inline">{schoolName ?? t('app.name')}</span>
        </Link>
        <nav
          aria-label={t('nav.label')}
          className="order-last -mx-2 flex w-full flex-wrap items-center pb-2 sm:order-none sm:mx-0 sm:w-auto sm:pb-0"
        >
          {/* Programme's navigation is words only; it leaves the icons out. */}
          {nav.map((item) => (
            <Link key={item.to} to={item.to} className={navLink}>
              {item.label}
            </Link>
          ))}
        </nav>
        <div className="flex min-w-0 items-center gap-1">
          {tools}
          {account}
          {language}
        </div>
      </div>
    </header>
  );
}
