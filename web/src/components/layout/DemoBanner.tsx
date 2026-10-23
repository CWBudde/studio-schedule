import { ExternalLink, FlaskConical } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { demoDocsUrl, isDemo } from '@/lib/env';

/** Persistent notice shown only in the WASM demo build. */
export function DemoBanner() {
  const { t } = useTranslation();
  if (!isDemo) return null;

  return (
    <aside
      aria-label={t('demo.label')}
      className="border-b border-amber-300/70 bg-amber-50 text-amber-950 dark:border-amber-500/30 dark:bg-amber-950/40 dark:text-amber-100"
    >
      <div className="mx-auto flex max-w-5xl items-start gap-3 px-4 py-2.5 text-sm sm:items-center">
        <FlaskConical className="mt-0.5 size-4 shrink-0 sm:mt-0" aria-hidden="true" />
        <p className="flex-1 leading-snug">
          {t('demo.banner')}{' '}
          <a
            href={demoDocsUrl}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={t('demo.learnMoreLabel')}
            className="inline-flex items-center gap-1 font-medium whitespace-nowrap underline underline-offset-4 hover:no-underline focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-current"
          >
            {t('demo.learnMore')}
            <ExternalLink className="size-3.5" aria-hidden="true" />
          </a>
        </p>
      </div>
    </aside>
  );
}
