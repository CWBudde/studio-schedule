import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';
import deAccount from './locales/de/account.json';
import deAuth from './locales/de/auth.json';
import de from './locales/de/common.json';
import deSchedule from './locales/de/schedule.json';
import deTimetable from './locales/de/timetable.json';
import enAccount from './locales/en/account.json';
import enAuth from './locales/en/auth.json';
import en from './locales/en/common.json';
import enSchedule from './locales/en/schedule.json';
import enTimetable from './locales/en/timetable.json';

export const supportedLanguages = ['en', 'de'] as const;
export type SupportedLanguage = (typeof supportedLanguages)[number];

export const defaultNS = 'common';
export const resources = {
  en: {
    common: en,
    schedule: enSchedule,
    auth: enAuth,
    account: enAccount,
    timetable: enTimetable,
  },
  de: {
    common: de,
    schedule: deSchedule,
    auth: deAuth,
    account: deAccount,
    timetable: deTimetable,
  },
} as const;

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    defaultNS,
    ns: [defaultNS, 'schedule', 'auth', 'account', 'timetable'],
    fallbackLng: 'en',
    supportedLngs: supportedLanguages,
    nonExplicitSupportedLngs: true,
    load: 'languageOnly',
    // Resources are bundled, so initialise synchronously (no flash of keys).
    initAsync: false,
    interpolation: { escapeValue: false }, // React escapes already
    detection: {
      order: ['localStorage', 'navigator', 'htmlTag'],
      caches: ['localStorage'],
      lookupLocalStorage: 'studio-schedule.lang',
    },
    react: { useSuspense: false },
  });

function syncHtmlLang(lng: string | undefined) {
  if (typeof document !== 'undefined' && lng) document.documentElement.lang = lng;
}
syncHtmlLang(i18n.resolvedLanguage);
i18n.on('languageChanged', () => syncHtmlLang(i18n.resolvedLanguage));

/** The active UI language, normalised to one of the supported languages. */
export function currentLanguage(): SupportedLanguage {
  const lng = i18n.resolvedLanguage;
  return lng === 'de' ? 'de' : 'en';
}

export default i18n;
