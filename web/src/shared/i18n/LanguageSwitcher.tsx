import { useI18n } from './I18nProvider';

type LanguageSwitcherProps = {
  /** Stable test id for the shell or auth header control. */
  testId?: string;
  className?: string;
  /** Class for the visually hidden accessible label. */
  labelClassName?: string;
};

/**
 * EN/VI toggle for the app shell. The visible text is the current language;
 * the accessible name is the target language.
 */
export function LanguageSwitcher({
  testId = 'shell-lang-toggle',
  className,
  labelClassName = 'sr-only',
}: LanguageSwitcherProps) {
  const { t, locale, toggleLocale } = useI18n();

  return (
    <button type="button" className={className} onClick={toggleLocale} data-testid={testId}>
      <span aria-hidden="true">{locale.toUpperCase()}</span>
      <span className={labelClassName}>{t('shell.lang.switch')}</span>
    </button>
  );
}
