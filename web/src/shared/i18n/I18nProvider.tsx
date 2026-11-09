import { createContext, useCallback, useContext, useEffect, useMemo, useRef } from 'react';
import type { ReactNode } from 'react';
import { I18nextProvider, useTranslation } from 'react-i18next';
import {
  DEFAULT_LOCALE,
  i18nInstance,
  isLocale,
  otherLocale,
  persistLocale,
  splitMessageKey,
} from './init';
import type { Locale, MessageKey, MessageParams, Translate } from './init';

export type I18nValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  toggleLocale: () => void;
  t: Translate;
};

const I18nContext = createContext<I18nValue | null>(null);

type I18nBridgeProps = {
  children: ReactNode;
  /** Overrides the stored language; used by tests and by a future profile setting. */
  initialLocale?: Locale;
};

/**
 * Bridge from react-i18next into the existing `useI18n()` API. Mount
 * `I18nextProvider` once in `App` (and the test render helper).
 */
export function I18nBridge({ children, initialLocale }: I18nBridgeProps) {
  const { i18n, t: i18nT } = useTranslation();
  const lastInitial = useRef<Locale | undefined>(undefined);

  // Apply initialLocale only when the prop itself changes — never when the
  // user toggles language (that would snap the UI back to the prop).
  useEffect(() => {
    if (initialLocale && lastInitial.current !== initialLocale) {
      lastInitial.current = initialLocale;
      void i18nInstance.changeLanguage(initialLocale);
    }
  }, [initialLocale]);

  const locale: Locale = isLocale(i18n.language) ? i18n.language : DEFAULT_LOCALE;

  const setLocale = useCallback((next: Locale) => {
    persistLocale(next);
    void i18nInstance.changeLanguage(next);
  }, []);

  const t = useCallback<Translate>(
    (key, params) => {
      const { ns, key: rest } = splitMessageKey(key);
      return i18nT(rest as never, { ns: ns as never, ...params });
    },
    [i18nT],
  );

  const value = useMemo<I18nValue>(
    () => ({
      locale,
      setLocale,
      toggleLocale: () => {
        setLocale(otherLocale(locale));
      },
      t,
    }),
    [locale, setLocale, t],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (value === null) {
    throw new Error('useI18n must be used inside I18nBridge');
  }
  return value;
}

/**
 * Test and story helper that mounts `I18nextProvider` + `I18nBridge`.
 * Production mounts `I18nextProvider` once in `App.tsx`.
 */
export function I18nProvider({ children, initialLocale }: I18nBridgeProps) {
  return (
    <I18nextProvider i18n={i18nInstance}>
      <I18nBridge initialLocale={initialLocale}>{children}</I18nBridge>
    </I18nextProvider>
  );
}

export type { MessageKey, MessageParams, Translate };
