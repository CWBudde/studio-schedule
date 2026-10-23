import { Link } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { DemoMailbox } from '@/features/demo/DemoMailbox';
import { AccountMenu } from './AccountMenu';
import { BrandMark } from './BrandMark';
import { LanguageSwitcher } from './LanguageSwitcher';

// Programme-style small capitals; the current page is underlined in the primary color.
const navLink =
  'relative rounded-sm px-2 py-1.5 text-xs font-medium tracking-[0.16em] text-muted-foreground uppercase transition-colors after:absolute after:inset-x-2 after:bottom-0 after:h-px after:origin-left after:scale-x-0 after:bg-primary after:transition-transform hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none data-[status=active]:text-primary data-[status=active]:after:scale-x-100 motion-reduce:after:transition-none sm:px-3 sm:after:inset-x-3';

export function AppHeader() {
  const { t } = useTranslation();

  // Two hairlines at the bottom, like the rule under a programme's masthead.
  return (
    <header className="border-b bg-background/80 shadow-[0_3px_0_-2px_var(--border)] backdrop-blur supports-[backdrop-filter]:bg-background/70">
      {/* One row on wider screens; on phones the main navigation gets a row of its own. */}
      <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-1 px-4">
        <Link
          to="/"
          aria-label={t('app.home')}
          className="mr-auto flex h-16 items-center gap-2.5 rounded-md font-heading text-xl font-semibold tracking-tight focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <BrandMark className="size-9" />
          <span className="hidden sm:inline">{t('app.name')}</span>
        </Link>
        <nav
          aria-label={t('nav.label')}
          className="order-last -mx-2 flex w-full items-center pb-2 sm:order-none sm:mx-0 sm:w-auto sm:pb-0"
        >
          <Link to="/schedule" className={navLink}>
            {t('nav.schedule')}
          </Link>
          <Link to="/classes" className={navLink}>
            {t('nav.classes')}
          </Link>
        </nav>
        <div className="flex min-w-0 items-center gap-1">
          <DemoMailbox />
          <AccountMenu />
          <LanguageSwitcher />
        </div>
      </div>
    </header>
  );
}
