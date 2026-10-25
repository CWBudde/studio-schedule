import { useQuery } from '@tanstack/react-query';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useBackendStatus } from '@/api/backendStatus';
import { publicSettingsQuery, schoolLogoQuery } from '@/api/queries';
import { applySchoolLanguage } from '@/i18n';
import { useDataUrl } from '@/lib/useDataUrl';

/** The public settings, once the backend runs (the header and footer render before). */
export function usePublicSettings() {
  const backend = useBackendStatus();
  return useQuery({ ...publicSettingsQuery, enabled: backend.state === 'ready' });
}

/** The school's logo as a data URL, while it has one. */
export function useSchoolLogoUrl(hasLogo: boolean | undefined): string | undefined {
  const logo = useQuery({ ...schoolLogoQuery, enabled: hasLogo === true });
  return useDataUrl(hasLogo ? logo.data : null);
}

const styleId = 'school-accent';

/**
 * The browser color of web/index.html, kept on its element before an accent
 * replaces it.
 */
function appThemeColor(meta: HTMLMetaElement): string {
  meta.dataset.appColor ??= meta.content;
  return meta.dataset.appColor;
}

/**
 * The style sheet of an accent: the primary color and what follows it, and
 * in the dark theme the derived variant (the light accent stays where light
 * text is on it, like today's day heading). `html:root` and `html.dark` win
 * over the theme's `:root` and `.dark` (web/src/index.css).
 */
export function accentStyleSheet(light: string, dark: string): string {
  const vars = (c: string) =>
    `--primary:${c};--ring:${c};--sidebar-primary:${c};--sidebar-ring:${c};`;
  return `html:root{${vars(light)}--day-today:${light};}html.dark{${vars(dark)}--day-today:${light};}`;
}

/**
 * Applies the school's settings to the page: its name as the document
 * title (else the app's, in the UI language), its accent color as the
 * primary color (else the theme's own), and its default language for a
 * visitor who chose none (ADR 0013).
 */
export function useApplyBranding(
  settings:
    | { schoolName: string; accentColor?: string; accentColorDark?: string; defaultLocale?: string }
    | undefined,
) {
  const { t } = useTranslation();
  const name = settings?.schoolName || t('app.name');
  useEffect(() => {
    document.title = name;
  }, [name]);

  const locale = settings?.defaultLocale;
  useEffect(() => applySchoolLanguage(locale), [locale]);

  const light = settings?.accentColor;
  const dark = settings?.accentColorDark;
  useEffect(() => {
    let style = document.getElementById(styleId);
    const themeColor = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    const appColor = themeColor ? appThemeColor(themeColor) : undefined;
    if (!light || !dark) {
      style?.remove();
      if (themeColor && appColor) themeColor.content = appColor;
      return;
    }
    if (!style) {
      style = document.createElement('style');
      style.id = styleId;
      document.head.append(style);
    }
    style.textContent = accentStyleSheet(light, dark);
    if (themeColor) themeColor.content = light;
  }, [light, dark]);
}
