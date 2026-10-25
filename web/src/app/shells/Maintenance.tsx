import { useCallback, useEffect, useRef, useState } from 'react';
import { Icon, useI18n } from '../../shared';
import { getHealth } from '../../shared/api/gen/common/common';
import styles from './shells.module.css';

export const PROBE_FAILURES_TO_DOWN = 3;
export const PROBE_BASE_MS = 15_000;
export const PROBE_MAX_MS = 60_000;

type Maintenance = { down: boolean; checkedAt: Date | null; retryNow: () => void };

async function probe(): Promise<boolean> {
  try {
    const response = await getHealth({ cache: 'no-store' });
    return response.status >= 200 && response.status < 300;
  } catch {
    // A network failure and an ApiShapeError (an SPA fallback page) are both a failed probe.
    return false;
  }
}

/**
 * Probes GET /healthz. Three failures in a row mean maintenance; while down it
 * retries every 15 s with backoff to 60 s and returns to normal on the first success.
 */
export function useMaintenance(enabled: boolean): Maintenance {
  const [down, setDown] = useState(false);
  const [checkedAt, setCheckedAt] = useState<Date | null>(null);
  const failures = useRef(0);
  const timer = useRef<number | undefined>(undefined);
  const runRef = useRef<() => Promise<void>>(() => Promise.resolve());

  useEffect(() => {
    if (!enabled) return undefined;
    let cancelled = false;
    const run = async () => {
      window.clearTimeout(timer.current);
      const healthy = await probe();
      if (cancelled) return;
      failures.current = healthy ? 0 : failures.current + 1;
      const isDown = failures.current >= PROBE_FAILURES_TO_DOWN;
      setDown(isDown);
      setCheckedAt(new Date());
      const backoff = Math.min(
        PROBE_MAX_MS,
        PROBE_BASE_MS * 2 ** Math.max(0, failures.current - PROBE_FAILURES_TO_DOWN),
      );
      timer.current = window.setTimeout(() => {
        void run();
      }, backoff);
    };
    runRef.current = run;
    void run();
    return () => {
      cancelled = true;
      window.clearTimeout(timer.current);
    };
  }, [enabled]);

  const retryNow = useCallback(() => {
    void runRef.current();
  }, []);

  return { down, checkedAt, retryNow };
}

type MaintenancePageProps = { checkedAt: Date | null; onRetry: () => void };

/** Full page without tabs; the shell replaces everything else with it. */
export function MaintenancePage({ checkedAt, onRetry }: MaintenancePageProps) {
  const { t, locale } = useI18n();
  const time =
    checkedAt === null
      ? ''
      : new Intl.DateTimeFormat(locale === 'vi' ? 'vi-VN' : 'en-US', {
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        }).format(checkedAt);

  return (
    <main id="main" className={styles.fullPage} data-testid="shell-maintenance">
      <Icon name="gear" size={40} />
      <h1 tabIndex={-1}>{t('shell.maintenance.title')}</h1>
      <p>{t('shell.maintenance.text')}</p>
      <p className={styles.muted}>{t('shell.maintenance.checked', { time })}</p>
      <button
        type="button"
        className={`${styles.btn} ${styles.primary}`}
        onClick={onRetry}
        data-testid="shell-maintenance-retry"
      >
        {t('shell.maintenance.retry')}
      </button>
    </main>
  );
}
