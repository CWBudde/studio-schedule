import { Link } from '@tanstack/react-router';
import { FlaskConical } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { isDemo } from '@/lib/env';

/** Persistent notice shown only in the WASM demo build. */
export function DemoBanner() {
  const { t } = useTranslation();
  if (!isDemo) return null;

  return (
    <aside
      aria-label={t('demo.label')}
      className="border-b border-primary/10 bg-secondary/60 text-secondary-foreground"
    >
      <div className="mx-auto flex max-w-5xl items-start gap-3 px-4 py-2.5 text-sm sm:items-center">
        <FlaskConical className="mt-0.5 size-4 shrink-0 sm:mt-0" aria-hidden="true" />
        <p className="flex-1 leading-snug">
          {t('demo.banner')}{' '}
          <Link
            to="/about"
            className="font-medium whitespace-nowrap underline underline-offset-4 hover:no-underline focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-current"
          >
            {t('demo.about')}
          </Link>
        </p>
      </div>
    </aside>
  );
}
