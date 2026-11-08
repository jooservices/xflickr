export {
  DEFAULT_LOCALE,
  LOCALES,
  LOCALE_STORAGE_KEY,
  NAMESPACES,
  catalogs,
  glossaryTerms,
  i18nInstance,
  isLocale,
  otherLocale,
  persistLocale,
  readStoredLocale,
  resources,
  splitMessageKey,
  translate,
  translateErrorCode,
} from './init';
export type { Locale, MessageKey, MessageParams, Namespace, Translate } from './init';
export { I18nBridge, I18nProvider, useI18n } from './I18nProvider';
export type { I18nValue } from './I18nProvider';
export { LanguageSwitcher } from './LanguageSwitcher';
export { DataText, Term, isGlossaryTerm } from './Term';
