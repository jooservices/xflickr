import { Link } from 'react-router';
import { useI18n } from '../../shared';
import type { AdminShellData } from './data';
import styles from './shells.module.css';

export const STALE_AFTER_MS = 20_000;

type Cluster = AdminShellData['cluster'];

function Sparkline({ values, label }: { values: number[]; label: string }) {
  const max = Math.max(1, ...values);
  const step = values.length > 1 ? 100 / (values.length - 1) : 0;
  const points = values
    .map((value, index) => `${(index * step).toFixed(2)},${(18 - (value / max) * 16).toFixed(2)}`)
    .join(' ');

  return (
    <svg
      className={styles.spark}
      viewBox="0 0 100 20"
      preserveAspectRatio="none"
      role="img"
      aria-label={label}
      data-testid="admin-cluster-spark"
    >
      <polyline fill="none" stroke="var(--s1)" strokeWidth="1.5" points={points} />
    </svg>
  );
}

type ClusterFooterProps = {
  state: 'loading' | 'error' | 'ready';
  cluster?: Cluster;
  /** Milliseconds since the last successful answer. */
  ageMs?: number;
  liveOff: boolean;
  onRetry: () => void;
};

/** Cluster health footer of the rail: loading, error, empty (no nodes) and ready. */
export function ClusterFooter({ state, cluster, ageMs = 0, liveOff, onRetry }: ClusterFooterProps) {
  const { t, locale } = useI18n();

  if (state === 'loading') {
    return (
      <div className={styles.railFoot} aria-busy="true" data-testid="admin-cluster-footer">
        <div data-testid="admin-cluster-loading">
          <span className={styles.eyebrow}>{t('admin.shell.cluster.title')}</span>
          <div className={styles.skel} style={{ width: 64, height: 18 }} />
          <div className={styles.skel} style={{ width: '70%', height: 12, marginTop: 6 }} />
          <div className={styles.skel} style={{ width: '100%', height: 20, marginTop: 6 }} />
          <span className={styles.srOnly}>{t('admin.shell.loading.text')}</span>
        </div>
      </div>
    );
  }

  if (state === 'error' || cluster === undefined) {
    return (
      <div className={styles.railFoot} data-testid="admin-cluster-footer">
        <div
          className={`${styles.banner} ${styles.bad}`}
          role="alert"
          data-testid="admin-cluster-error"
        >
          <span>
            <strong>{t('admin.shell.error.title')}</strong>
            <br />
            <small>{t('admin.shell.error.text')}</small>
          </span>
          <button
            type="button"
            className={styles.btn}
            onClick={onRetry}
            data-testid="admin-cluster-retry"
          >
            {t('common.retry')}
          </button>
        </div>
      </div>
    );
  }

  if (cluster.status === 'unknown' && cluster.nodes_total === 0) {
    return (
      <div className={styles.railFoot} data-testid="admin-cluster-footer">
        <div data-testid="admin-cluster-empty">
          <span className={styles.eyebrow}>{t('admin.shell.cluster.title')}</span>
          <span className={styles.pill} data-testid="admin-cluster-status">
            {t('admin.shell.cluster.no-nodes')}
          </span>
          <p className={styles.muted}>{t('admin.shell.empty.title')}</p>
          <Link className={styles.btn} to="/admin/nodes">
            {t('admin.shell.empty.cta')}
          </Link>
        </div>
      </div>
    );
  }

  const statusKey = {
    ok: 'admin.shell.cluster.healthy',
    degraded: 'admin.shell.cluster.degraded',
    down: 'admin.shell.cluster.down',
    unknown: 'admin.shell.cluster.no-nodes',
  } as const;
  const stale = liveOff || ageMs > STALE_AFTER_MS;
  const seconds = Math.round(ageMs / 1000);
  const age = new Intl.RelativeTimeFormat(locale, { numeric: 'always', style: 'short' }).format(
    -seconds,
    'second',
  );

  return (
    <Link to="/admin/nodes" className={styles.railFoot} data-testid="admin-cluster-footer">
      <span className={styles.eyebrow}>{t('admin.shell.cluster.title')}</span>
      <span className={styles.row}>
        <span
          className={`${styles.pill} ${cluster.status === 'ok' ? '' : cluster.status === 'degraded' ? styles.warnPill : styles.badPill}`}
          data-testid="admin-cluster-status"
        >
          {t(statusKey[cluster.status])}
        </span>
        {stale ? (
          <span className={styles.pill} data-testid="admin-cluster-stale">
            {t('admin.shell.cluster.stale')}
          </span>
        ) : null}
      </span>
      <span className={styles.num} data-testid="admin-cluster-summary">
        {t('admin.shell.cluster.summary', {
          calls: cluster.calls_per_second,
          healthy: cluster.nodes_healthy,
          total: cluster.nodes_total,
        })}
      </span>
      {stale ? (
        <span className={styles.muted}>{t('admin.shell.cluster.updated', { age })}</span>
      ) : null}
      <Sparkline
        values={cluster.calls_history}
        label={t('admin.shell.cluster.chart-alt', { calls: cluster.calls_per_second })}
      />
    </Link>
  );
}
