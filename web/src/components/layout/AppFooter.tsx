import { useTranslation } from 'react-i18next';
import { useMe } from '@/api/useMe';
import { repositoryUrl } from '@/lib/env';
import { usePublicSettings } from './branding';

export function AppFooter() {
  const { t } = useTranslation();
  const { t: tSettings } = useTranslation('settings');
  // Only the office and admins learn the version, for support (ASVS 14.3.3).
  const version = useMe().data?.version;
  const legalNotice = usePublicSettings().data?.legalNotice;

  return (
    <footer className="border-t">
      {legalNotice && (
        <section
          aria-label={tSettings('footer.legalNotice')}
          className="mx-auto max-w-5xl px-4 pt-4"
        >
          <p className="text-xs whitespace-pre-line text-muted-foreground">{legalNotice}</p>
        </section>
      )}
      <div className="mx-auto flex max-w-5xl flex-col gap-1 px-4 py-4 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
        <p>
          {t('app.name')}
          {version && <> · {t('footer.version', { version })}</>}
        </p>
        <a
          href={repositoryUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="underline-offset-4 hover:text-foreground hover:underline"
        >
          {t('footer.source')}
        </a>
      </div>
    </footer>
  );
}
