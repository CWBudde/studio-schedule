import { type ReactNode, useEffect, useLayoutEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { PageLoading } from '@/components/layout/BackendGate';
import { usePublicSettings } from '@/components/layout/branding';
import { programme } from './programme';
import { loadTheme } from './registry';
import type { Theme, ThemeId } from './slots';
import { cachedTheme, cacheTheme } from './themeCache';
import { ThemeContext } from './themeContext';

/** A theme load that settled: the theme, or Programme in its place after a failure. */
interface Settled {
  id: ThemeId;
  theme: Theme;
}

/**
 * Provides the school's theme to the slots (ADR 0018): the public settings'
 * theme, before they arrive the one cached by the last visit, else
 * Programme. Programme is there at once; while another theme's chunk
 * loads, a loading state shows instead of the app. A theme that fails to
 * load (offline, say) is replaced by Programme. The active theme's id is on
 * `<html data-theme>`; the school's is cached for the next start (also for
 * web/public/theme.js).
 *
 * `load` is for tests, which register themes of their own; it must keep
 * its identity across renders.
 */
export function ThemeProvider({
  children,
  load = loadTheme,
}: {
  children: ReactNode;
  load?: (id: ThemeId) => Promise<Theme>;
}) {
  const { t } = useTranslation();
  const school = usePublicSettings().data?.theme;
  const [cached] = useState(cachedTheme);
  const target = school ?? cached ?? programme.id;

  const [settled, setSettled] = useState<Settled>();
  useEffect(() => {
    if (target === programme.id) return;
    // A load for an earlier target that settles late is ignored.
    let current = true;
    load(target).then(
      (theme) => {
        if (current) setSettled({ id: target, theme });
      },
      (err: unknown) => {
        if (!current) return;
        console.warn(
          `[studio-schedule] could not load the theme ${target}, showing Programme`,
          err,
        );
        setSettled({ id: target, theme: programme });
      },
    );
    return () => {
      current = false;
    };
  }, [target, load]);

  const active =
    target === programme.id ? programme : settled?.id === target ? settled.theme : undefined;
  const activeId = active?.id;

  useLayoutEffect(() => {
    if (activeId) document.documentElement.dataset.theme = activeId;
  }, [activeId]);
  useLayoutEffect(
    () => () => {
      delete document.documentElement.dataset.theme;
    },
    [],
  );
  useEffect(() => {
    // Only the school's own theme is kept, once it shows (not Programme in place of a failed one).
    if (school && activeId === school) cacheTheme(school);
  }, [school, activeId]);

  if (!active) {
    return (
      <div className="px-4 py-8">
        <PageLoading title={t('theme.loading')} />
      </div>
    );
  }
  return <ThemeContext.Provider value={active}>{children}</ThemeContext.Provider>;
}
