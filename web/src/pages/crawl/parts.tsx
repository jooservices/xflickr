import type { ReactNode } from 'react';
import { Icon, useI18n } from '../../shared';
import type { IconName } from '../../shared';
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
