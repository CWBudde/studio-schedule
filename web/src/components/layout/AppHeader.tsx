import { Link } from '@tanstack/react-router';
import { CalendarHeart } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { DemoMailbox } from '@/features/demo/DemoMailbox';
import { AccountMenu } from './AccountMenu';
import { LanguageSwitcher } from './LanguageSwitcher';

const navLink =
  'rounded-md px-2 py-1.5 text-sm font-medium text-muted-foreground hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none data-[status=active]:text-foreground sm:px-3';

export function AppHeader() {
  const { t } = useTranslation();

  return (
    <header className="border-b bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/70">
      {/* One row on wider screens; on phones the main navigation gets a row of its own. */}
      <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-1 px-4">
        <Link
          to="/"
          aria-label={t('app.home')}
          className="mr-auto flex h-14 items-center gap-2 rounded-md font-heading text-base font-semibold tracking-tight focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
            <CalendarHeart className="size-4.5" aria-hidden="true" />
          </span>
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
