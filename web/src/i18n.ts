import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';
import deAccount from './locales/de/account.json';
import deAdmin from './locales/de/admin.json';
import deAnnouncements from './locales/de/announcements.json';
import deAuth from './locales/de/auth.json';
import deFeeds from './locales/de/feeds.json';
import deMailLog from './locales/de/mailLog.json';
import deMyLessons from './locales/de/myLessons.json';
import dePasses from './locales/de/passes.json';
import deReceipts from './locales/de/receipts.json';
import de from './locales/de/common.json';
import deSchedule from './locales/de/schedule.json';
import deSettings from './locales/de/settings.json';
import deStatistics from './locales/de/statistics.json';
import deTeach from './locales/de/teach.json';
import deTimetable from './locales/de/timetable.json';
import enAccount from './locales/en/account.json';
import enAdmin from './locales/en/admin.json';
import enAnnouncements from './locales/en/announcements.json';
import enAuth from './locales/en/auth.json';
import enFeeds from './locales/en/feeds.json';
import enMailLog from './locales/en/mailLog.json';
import enMyLessons from './locales/en/myLessons.json';
import enPasses from './locales/en/passes.json';
import enReceipts from './locales/en/receipts.json';
import en from './locales/en/common.json';
import enSchedule from './locales/en/schedule.json';
import enSettings from './locales/en/settings.json';
import enStatistics from './locales/en/statistics.json';
import enTeach from './locales/en/teach.json';
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
    admin: enAdmin,
    teach: enTeach,
    myLessons: enMyLessons,
    feeds: enFeeds,
    passes: enPasses,
    receipts: enReceipts,
    settings: enSettings,
    statistics: enStatistics,
    announcements: enAnnouncements,
    mailLog: enMailLog,
  },
  de: {
    common: de,
    schedule: deSchedule,
    auth: deAuth,
    account: deAccount,
    timetable: deTimetable,
    admin: deAdmin,
    teach: deTeach,
    myLessons: deMyLessons,
    feeds: deFeeds,
    passes: dePasses,
    receipts: deReceipts,
    settings: deSettings,
    statistics: deStatistics,
    announcements: deAnnouncements,
    mailLog: deMailLog,
  },
} as const;

/** Where the language a visitor chose in the header's switcher is kept. */
const chosenLanguageKey = 'studio-schedule.lang';
/** Where the school's default language is remembered for the next page load. */
const schoolLanguageKey = 'studio-schedule.school-lang';

// Storage can be unavailable (private windows, blocked site data).
function readStorage(key: string): string | undefined {
  try {
    return localStorage.getItem(key) ?? undefined;
  } catch {
    return undefined;
  }
}

function writeStorage(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // The choice then lasts for this page only.
  }
}

/** The supported language a language tag asks for ('de-AT' is 'de'), or undefined. */
export function supportedLanguage(tag: string | undefined): SupportedLanguage | undefined {
  const base = tag?.trim().toLowerCase().replace('_', '-').split('-')[0];
  return supportedLanguages.find((lng) => lng === base);
}

// The UI language is the visitor's own choice, else the school's default
// language (ADR 0013), remembered from the last page load until the public
// settings arrive, else the browser's. Only the switcher stores a choice: a
// detected language is not cached, so it never hides the school's default.
const languageDetector = new LanguageDetector();
languageDetector.addDetector({
  name: 'schoolDefault',
  lookup: () => readStorage(schoolLanguageKey),
});

void i18n
  .use(languageDetector)
  .use(initReactI18next)
  .init({
    resources,
    defaultNS,
    ns: [
      defaultNS,
      'schedule',
      'auth',
      'account',
      'timetable',
      'admin',
      'teach',
      'myLessons',
      'feeds',
      'passes',
      'receipts',
      'settings',
      'statistics',
      'announcements',
      'mailLog',
    ],
    fallbackLng: 'en',
    supportedLngs: supportedLanguages,
    nonExplicitSupportedLngs: true,
    load: 'languageOnly',
    // Resources are bundled, so initialise synchronously (no flash of keys).
    initAsync: false,
    interpolation: { escapeValue: false }, // React escapes already
    detection: {
      order: ['localStorage', 'schoolDefault', 'navigator', 'htmlTag'],
      caches: [],
      lookupLocalStorage: chosenLanguageKey,
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

/** Switches the UI to the language the visitor chose; it stays their choice. */
export function chooseLanguage(lng: SupportedLanguage) {
  writeStorage(chosenLanguageKey, lng);
  return i18n.changeLanguage(lng);
}

/**
 * Follows the school's default language (the public settings), unless the
 * visitor chose a language of their own (in the switcher, or by signing in
 * to an account with a language of its own).
 */
export function applySchoolLanguage(defaultLocale: string | undefined) {
  const lng = supportedLanguage(defaultLocale);
  if (!lng) return;
  writeStorage(schoolLanguageKey, lng);
  if (!supportedLanguage(readStorage(chosenLanguageKey)) && currentLanguage() !== lng) {
    void i18n.changeLanguage(lng);
  }
}

export default i18n;
