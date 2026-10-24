import en from './en.json';
import vi from './vi.json';

export const LOCALES = ['en', 'vi'] as const;
export type Locale = (typeof LOCALES)[number];

export type MessageKey = keyof typeof en;

export const catalogs: Record<Locale, Record<string, string>> = { en, vi };

export const DEFAULT_LOCALE: Locale = 'en';

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (LOCALES as readonly string[]).includes(value);
}

/** The other language, used by the EN/VI toggle. */
export function otherLocale(locale: Locale): Locale {
  return locale === 'en' ? 'vi' : 'en';
}
