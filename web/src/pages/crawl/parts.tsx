import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { Icon, useI18n } from '../../shared';
import type { IconName } from '../../shared';
import { formatCountdown, useNow } from './format';
import styles from './CrawlPage.module.css';

export type Tone = 'ok' | 'warn' | 'bad' | 'info' | 'neutral';

export function Pill({
  tone,
  children,
  testId,
}: {
  tone: Tone;
  children: ReactNode;
  testId?: string;
}) {
  return (
    <span
      className={tone === 'neutral' ? styles.pill : `${styles.pill} ${styles[`tone-${tone}`]}`}
      data-testid={testId}
    >
      {children}
    </span>
  );
}

type BarProps = {
  value: number;
  max: number;
  /** Spoken value, for example "5 of 10 done". */
  text: string;
  /** Accessible name: what the bar measures. */
  label: string;
  testId?: string;
};

/** Single-colour progress bar of value/max. */
export function Bar({ value, max, text, label, testId }: BarProps) {
  const percent = max <= 0 ? 0 : Math.min(100, Math.round((value / max) * 100));
  return (
    <div
      className={styles.bar}
      role="progressbar"
      aria-valuemin={0}
      aria-valuenow={value}
      aria-valuemax={max}
      aria-valuetext={text}
      aria-label={label}
      data-testid={testId}
    >
      <i style={{ width: `${String(percent)}%` }} />
    </div>
  );
}

export function SectionError({ name, onRetry }: { name: string; onRetry: () => void }) {
  const { t } = useI18n();
  return (
    <div
      className={styles.sectionError}
      role="alert"
      data-testid={`user-crawl-section-error-${name}`}
    >
      <Icon name="alert" size={16} />
      <span>{t('user.crawl.error.section')}</span>
      <button
        type="button"
        className={`${styles.btn} ${styles.sm}`}
        onClick={onRetry}
        data-testid={`user-crawl-section-retry-${name}`}
      >
        {t('common.retry')}
      </button>
    </div>
  );
}

export function SkeletonBlock({ height = 120 }: { height?: number }) {
  return <div className={styles.skel} style={{ height }} aria-hidden="true" />;
}

type BannerProps = {
  tone: 'warn' | 'bad' | 'info';
  role: 'status' | 'alert';
  testId: string;
  icon?: IconName;
  children: ReactNode;
  action?: ReactNode;
};

export function Banner({ tone, role, testId, icon, children, action }: BannerProps) {
  return (
    <div
      className={`${styles.banner} ${styles[`banner-${tone}`]}`}
      role={role}
      data-testid={testId}
    >
      <Icon name={icon ?? (tone === 'info' ? 'info' : 'alert')} size={18} />
      <div>{children}</div>
      {action}
    </div>
  );
}

type CountdownProps = {
  /** Epoch ms of the response (the query's `dataUpdatedAt`). */
  receivedAt: number;
  resetsAt: string;
  serverTime: string;
  /** The server clock is not trusted while the numbers are stale. */
  stale: boolean;
  onZero: () => void;
};

/**
 * `mm:ss` until `resets_at`, counted from the server clock plus the time elapsed since the
 * response. At zero it asks for fresh data at once and again after 3 s.
 */
export function Countdown({ receivedAt, resetsAt, serverTime, stale, onZero }: CountdownProps) {
  const now = useNow();
  const remaining = Date.parse(resetsAt) - (Date.parse(serverTime) + (now - receivedAt));
  const zero = !stale && remaining <= 0;
  const onZeroRef = useRef(onZero);
  const firedFor = useRef('');
  const timer = useRef<number>(0);

  useEffect(() => {
    onZeroRef.current = onZero;
  }, [onZero]);

  useEffect(() => {
    if (!zero || firedFor.current === resetsAt) return;
    firedFor.current = resetsAt;
    onZeroRef.current();
    timer.current = window.setTimeout(() => {
      onZeroRef.current();
    }, 3000);
  }, [zero, resetsAt]);

  useEffect(
    () => () => {
      window.clearTimeout(timer.current);
    },
    [],
  );

  return (
    <b className={styles.num} data-testid="user-crawl-banner-countdown">
      {stale ? '--:--' : formatCountdown(remaining)}
    </b>
  );
}
