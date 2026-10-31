import { Link } from '@tanstack/react-router';
import { cn } from 'cn';
import { FlaskConical } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { switchSchool } from '@/api/transport';
import { demoSchool } from '@/features/demo/school';
import { isDemo } from '@/lib/env';

const linkClass =
  'font-medium whitespace-nowrap underline underline-offset-4 hover:no-underline focus-visible:rounded-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-current';

/** Persistent notice shown only in the WASM demo build. */
export function DemoBanner() {
  const { t } = useTranslation();
  if (!isDemo) return null;
  const other = demoSchool() === 'werkstatt' ? 'programme' : 'werkstatt';

  return (
    <aside
      aria-label={t('demo.label')}
      className="border-b border-primary/10 bg-secondary/60 text-secondary-foreground"
    >
      <div className="mx-auto flex max-w-5xl items-start gap-3 px-4 py-2.5 text-sm sm:items-center">
        <FlaskConical className="mt-0.5 size-4 shrink-0 sm:mt-0" aria-hidden="true" />
        <p className="flex-1 leading-snug">
          {t('demo.banner')}{' '}
          <Link to="/about" className={linkClass}>
            {t('demo.about')}
          </Link>{' '}
          {/* A button that looks like its neighbour: switching is an action
              (it stores the choice and reloads the page), with no address
              of its own to open in a new tab. */}
          <button
            type="button"
            className={cn(linkClass, 'cursor-pointer')}
            onClick={() => void switchSchool(other)}
          >
            {t(other === 'werkstatt' ? 'demo.switchToWerkstatt' : 'demo.switchToProgramme')}
          </button>
        </p>
      </div>
    </aside>
  );
}
