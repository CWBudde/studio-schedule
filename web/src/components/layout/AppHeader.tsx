import { Link } from '@tanstack/react-router';
import { CalendarHeart } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { LanguageSwitcher } from './LanguageSwitcher';

export function AppHeader() {
  const { t } = useTranslation();

  return (
    <header className="border-b bg-background/80 backdrop-blur supports-[backdrop-filter]:bg-background/70">
      <div className="mx-auto flex h-14 max-w-5xl items-center justify-between gap-4 px-4">
        <Link
          to="/"
          aria-label={t('app.home')}
          className="flex items-center gap-2 rounded-md font-heading text-base font-semibold tracking-tight focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          <span className="flex size-8 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
            <CalendarHeart className="size-4.5" aria-hidden="true" />
          </span>
          <span>{t('app.name')}</span>
        </Link>
        <LanguageSwitcher />
      </div>
    </header>
  );
}
