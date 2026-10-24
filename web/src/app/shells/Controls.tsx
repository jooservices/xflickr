import { Icon, useI18n } from '../../shared';
import type { Theme } from '../../shared';
import { useShellRuntime } from './runtime';
import styles from './shells.module.css';

type ControlProps = { testId: string };

/** Live/Paused toggle: a dot plus the word, `aria-pressed` while live. */
export function LiveToggle({ testId }: ControlProps) {
  const { t } = useI18n();
  const { liveOff, livePaused, offline, toggleLive } = useShellRuntime();
  const reason = offline ? t('shell.offline.title') : undefined;
  const title = liveOff
    ? t('shell.live.reason', { reason: reason ?? t('shell.live.paused-toast') })
    : undefined;

  return (
    <button
      type="button"
      className={`${styles.txtBtn} ${liveOff ? styles.paused : ''}`}
      aria-pressed={!livePaused}
      title={title}
      onClick={toggleLive}
      data-testid={testId}
    >
      <span className={styles.liveDot} aria-hidden="true" />
      <span className={styles.label}>{liveOff ? t('shell.live.paused') : t('shell.live.on')}</span>
    </button>
  );
}

/**
 * EN/VI toggle. The visible text is the current language; the accessible name,
 * from hidden text so it never conflicts with the visible label, is the target.
 */
export function LangToggle({ testId }: ControlProps) {
  const { t, locale, toggleLocale } = useI18n();

  return (
    <button type="button" className={styles.txtBtn} onClick={toggleLocale} data-testid={testId}>
      <span aria-hidden="true">{locale.toUpperCase()}</span>
      <span className={styles.srOnly}>{t('shell.lang.switch')}</span>
    </button>
  );
}

type ThemeToggleProps = ControlProps & { theme: Theme; onToggle: () => void };

/** Icon button: moon in light, sun in dark. */
export function ThemeToggle({ testId, theme, onToggle }: ThemeToggleProps) {
  const { t } = useI18n();

  return (
    <button
      type="button"
      className={styles.iconBtn}
      aria-label={t('shell.theme.toggle')}
      aria-pressed={theme === 'dark'}
      onClick={onToggle}
      data-testid={testId}
    >
      <Icon name={theme === 'dark' ? 'sun' : 'moon'} />
    </button>
  );
}

type CountBadgeProps = { count: number | undefined; testId: string };

/** Count badge that is hidden at 0 and unknown, and shows `99+` above 99. */
export function CountBadge({ count, testId }: CountBadgeProps) {
  if (count === undefined || count <= 0) return null;
  return (
    <span className={styles.count} aria-hidden="true" data-testid={testId}>
      {count > 99 ? '99+' : count}
    </span>
  );
}
