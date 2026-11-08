export { AppTitle } from './ui/app-title';
export { Icon } from './ui/icon';
export type { IconName } from './ui/icon';
export {
  EmptyState,
  ErrorState,
  ForbiddenState,
  LoadingState,
  NotFoundState,
  PageState,
  StatePreview,
  VIEW_STATES,
  useLoadingTiming,
} from './ui/states';
export type { ViewState } from './ui/states';
export {
  DEFAULT_LOCALE,
  I18nBridge,
  I18nProvider,
  LanguageSwitcher,
  LOCALES,
  LOCALE_STORAGE_KEY,
  Term,
  catalogs,
  glossaryTerms,
  i18nInstance,
  isGlossaryTerm,
  isLocale,
  otherLocale,
  translate,
  translateErrorCode,
  useI18n,
} from './i18n';
export type { Locale, MessageKey, MessageParams, Translate } from './i18n';
export { useTheme } from './theme';
export type { Theme, ThemeRole } from './theme';

export const sharedLayer = 'shared';
