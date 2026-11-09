import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import adminEn from './locales/en/admin.json';
import authEn from './locales/en/auth.json';
import commonEn from './locales/en/common.json';
import crawlEn from './locales/en/crawl.json';
import errorsEn from './locales/en/errors.json';
import keysEn from './locales/en/keys.json';
import loginEn from './locales/en/login.json';
import profileEn from './locales/en/profile.json';
import setupEn from './locales/en/setup.json';
import shellEn from './locales/en/shell.json';
import adminVi from './locales/vi/admin.json';
import authVi from './locales/vi/auth.json';
import commonVi from './locales/vi/common.json';
import crawlVi from './locales/vi/crawl.json';
import errorsVi from './locales/vi/errors.json';
import keysVi from './locales/vi/keys.json';
import loginVi from './locales/vi/login.json';
import profileVi from './locales/vi/profile.json';
import setupVi from './locales/vi/setup.json';
import shellVi from './locales/vi/shell.json';
import glossaryJson from './glossary.json';

export const LOCALES = ['en', 'vi'] as const;
export type Locale = (typeof LOCALES)[number];
export const DEFAULT_LOCALE: Locale = 'en';
export const LOCALE_STORAGE_KEY = 'xf.locale';

export const NAMESPACES = [
  'admin',
  'auth',
  'common',
  'crawl',
  'errors',
  'keys',
  'login',
  'profile',
  'setup',
  'shell',
] as const;
export type Namespace = (typeof NAMESPACES)[number];

const enResources = {
  admin: adminEn,
  auth: authEn,
  common: commonEn,
  crawl: crawlEn,
  errors: errorsEn,
  keys: keysEn,
  login: loginEn,
  profile: profileEn,
  setup: setupEn,
  shell: shellEn,
} as const;

const viResources = {
  admin: adminVi,
  auth: authVi,
  common: commonVi,
  crawl: crawlVi,
  errors: errorsVi,
  keys: keysVi,
  login: loginVi,
  profile: profileVi,
  setup: setupVi,
  shell: shellVi,
} as const;

export const resources = {
  en: enResources,
  vi: viResources,
} as const;

function flattenLocale(nsMap: Record<string, Record<string, string>>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [ns, flat] of Object.entries(nsMap)) {
    for (const [key, value] of Object.entries(flat)) {
      out[`${ns}.${key}`] = value;
    }
  }
  return out;
}

/** Flat catalog map used by tests and the check-i18n expectations. */
export const catalogs: Record<Locale, Record<string, string>> = {
  en: flattenLocale(enResources),
  vi: flattenLocale(viResources),
};

export type MessageParams = Record<string, string | number | undefined>;
export type MessageKey = Extract<keyof typeof catalogs.en, string>;
export type Translate = (key: MessageKey, params?: MessageParams) => string;

export const glossaryTerms: readonly string[] = glossaryJson.terms;

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (LOCALES as readonly string[]).includes(value);
}

export function otherLocale(locale: Locale): Locale {
  return locale === 'en' ? 'vi' : 'en';
}

export function readStoredLocale(): Locale {
  try {
    const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY);
    return isLocale(stored) ? stored : DEFAULT_LOCALE;
  } catch {
    return DEFAULT_LOCALE;
  }
}

export function persistLocale(locale: Locale): void {
  try {
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  } catch {
    // Storage can be blocked; the choice then lasts for this page only.
  }
}

/** Split `shell.nav.library` into namespace + key for i18next. */
export function splitMessageKey(key: string): { ns: string; key: string } {
  const dot = key.indexOf('.');
  if (dot === -1) return { ns: 'common', key };
  return { ns: key.slice(0, dot), key: key.slice(dot + 1) };
}

function initI18n(): typeof i18n {
  if (i18n.isInitialized) return i18n;

  const initial = typeof window === 'undefined' ? DEFAULT_LOCALE : readStoredLocale();

  void i18n.use(initReactI18next).init({
    resources,
    lng: initial,
    fallbackLng: DEFAULT_LOCALE,
    defaultNS: 'common',
    ns: [...NAMESPACES],
    keySeparator: false,
    nsSeparator: ':',
    interpolation: {
      escapeValue: false,
    },
    returnNull: false,
    returnEmptyString: false,
    pluralSeparator: '_',
    compatibilityJSON: 'v4',
  });

  if (typeof document !== 'undefined') {
    document.documentElement.lang = i18n.language;
    i18n.on('languageChanged', (lng) => {
      document.documentElement.lang = lng;
    });
  }

  return i18n;
}

export const i18nInstance = initI18n();

/** Translate a dotted catalog key with the active (or given) language. */
export function translate(
  localeOrKey: Locale | MessageKey,
  keyOrParams?: MessageKey | MessageParams,
  params?: MessageParams,
): string {
  let locale: Locale = (i18nInstance.language as Locale) || DEFAULT_LOCALE;
  let key: string;
  let values: MessageParams | undefined;

  if (isLocale(localeOrKey) && typeof keyOrParams === 'string') {
    locale = localeOrKey;
    key = keyOrParams;
    values = params;
  } else {
    key = localeOrKey;
    values = keyOrParams as MessageParams | undefined;
  }

  const { ns, key: rest } = splitMessageKey(key);
  return i18nInstance.t(rest as never, {
    ns: ns as never,
    lng: locale,
    ...values,
  });
}

/**
 * Map a stable problem/API `code` (or problem type basename) to a translated
 * message. Unknown codes use the generic errors string — never a raw server
 * English sentence.
 */
export function translateErrorCode(
  code: string | null | undefined,
  t: Translate = (key, p) => translate(key, p),
): string {
  if (!code) return t('errors.generic');
  const normalized = code.startsWith('/problems/') ? code.slice('/problems/'.length) : code;
  const dotted = `errors.codes.${normalized}` as MessageKey;
  const { ns, key } = splitMessageKey(dotted);
  if (i18nInstance.exists(key, { ns: ns as (typeof NAMESPACES)[number] })) {
    return t(dotted);
  }
  return t('errors.generic');
}
