import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { DEFAULT_LOCALE, catalogs, isLocale, otherLocale } from './catalog';
import type { Locale, MessageKey } from './catalog';
import { formatMessage } from './format';
import type { MessageParams } from './format';

export const LOCALE_STORAGE_KEY = 'xf.locale';

export type Translate = (key: MessageKey, params?: MessageParams) => string;

type I18nValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
  t: Translate;
};

/** Looks a key up in the active catalog; a missing Vietnamese string falls back to English. */
export function translate(locale: Locale, key: MessageKey, params?: MessageParams): string {
  const message = catalogs[locale][key] ?? catalogs[DEFAULT_LOCALE][key];
  return message === undefined ? key : formatMessage(message, params, locale);
}

function readStoredLocale(): Locale {
  try {
    const stored = window.localStorage.getItem(LOCALE_STORAGE_KEY);
    return isLocale(stored) ? stored : DEFAULT_LOCALE;
  } catch {
    return DEFAULT_LOCALE;
  }
}

const I18nContext = createContext<I18nValue | null>(null);

type I18nProviderProps = {
  children: ReactNode;
  /** Overrides the stored language; used by tests and by a future profile setting. */
  initialLocale?: Locale;
};

/** Language lives in local state and localStorage until the profile can store it. */
export function I18nProvider({ children, initialLocale }: I18nProviderProps) {
  const [locale, setLocaleState] = useState<Locale>(initialLocale ?? readStoredLocale);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      window.localStorage.setItem(LOCALE_STORAGE_KEY, next);
    } catch {
      // Storage can be blocked; the choice then lasts for this page only.
    }
  }, []);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const value = useMemo<I18nValue>(
    () => ({
      locale,
      setLocale,
      toggleLocale: () => {
        setLocale(otherLocale(locale));
      },
      t: (key, params) => translate(locale, key, params),
    }),
    [locale, setLocale],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (value === null) {
    throw new Error('useI18n must be used inside I18nProvider');
  }
  return value;
}
