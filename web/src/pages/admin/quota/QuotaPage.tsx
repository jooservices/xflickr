import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router';
import type { AdminKeyQuota } from '../../../shared/api/gen/models';
import { EmptyState, ForbiddenState, Icon, useI18n, useLoadingTiming } from '../../../shared';
import type { Locale, MessageKey } from '../../../shared';
import styles from './QuotaPage.module.css';
import { useAdminQuota } from '../../../features/admin/quota';

function localeCode(locale: Locale): string {
  return locale === 'vi' ? 'vi-VN' : 'en-US';
}

function formatNumber(value: number, locale: Locale): string {
  return new Intl.NumberFormat(localeCode(locale)).format(value);
}

function formatAge(milliseconds: number, locale: Locale): string {
  const seconds = Math.max(0, Math.floor(milliseconds / 1000));
  const unit = seconds < 60 ? 'second' : 'minute';
  const value = unit === 'second' ? Math.max(1, seconds) : Math.floor(seconds / 60);
  return new Intl.NumberFormat(localeCode(locale), {
    style: 'unit',
    unit,
    unitDisplay: 'short',
  }).format(value);
}

function formatReset(resetAt: string | undefined, now: number, locale: Locale): string {
  if (resetAt === undefined) return '—';
  const milliseconds = Date.parse(resetAt) - now;
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return '—';
  const seconds = Math.ceil(milliseconds / 1000);
  const unit = seconds >= 3600 ? 'hour' : seconds >= 60 ? 'minute' : 'second';
  const value =
    unit === 'hour'
      ? Math.ceil(seconds / 3600)
      : unit === 'minute'
        ? Math.ceil(seconds / 60)
        : seconds;
  return new Intl.RelativeTimeFormat(localeCode(locale), {
    numeric: 'always',
    style: 'short',
  }).format(value, unit);
}

function stateCopy(key: AdminKeyQuota, t: (key: MessageKey) => string): string {
  switch (key.state) {
    case 'ok':
      return t('admin.quota.status-ok');
    case 'cooling':
      return t('admin.quota.status-cooling');
    case 'exhausted':
      return t('admin.quota.status-exhausted');
  }
}

function QuotaRow({
  keyData,
  now,
  locale,
}: {
  keyData: AdminKeyQuota;
  now: number;
  locale: Locale;
}) {
  const { t } = useI18n();
  const used = formatNumber(keyData.used, locale);
  const limit = formatNumber(keyData.limit, locale);
  const visibleUsed = Math.min(keyData.used, keyData.limit);
  const progress = Math.min(100, (visibleUsed / keyData.limit) * 100);
  const owner =
    keyData.user_id === undefined
      ? t('admin.quota.unassociated')
      : t('admin.quota.user-id', { id: String(keyData.user_id) });
  const reset = formatReset(keyData.reset_at, now, locale);

  return (
    <tr data-testid="admin-quota-key-row">
      <td data-label={t('admin.quota.key')} className={styles.keyCell}>
        <span>{keyData.label}</span>
        <code>#{keyData.key_id}</code>
      </td>
      <td data-label={t('admin.quota.owner')}>{owner}</td>
      <td data-label={t('admin.quota.node')}>
        <code>{keyData.node}</code>
      </td>
      <td data-label={t('admin.quota.used')}>
        <div
          className={styles.progress}
          role="progressbar"
          aria-label={t('admin.quota.used')}
          aria-valuemin={0}
          aria-valuemax={keyData.limit}
          aria-valuenow={visibleUsed}
          aria-valuetext={`${used} / ${limit}`}
        >
          <span style={{ width: `${progress}%` }} />
        </div>
        <span className={styles.numeric} data-testid="admin-quota-used">
          {used} / {limit}
        </span>
      </td>
      <td data-label={t('admin.quota.state')}>
        <span className={`${styles.state} ${styles[keyData.state]}`}>
          <span aria-hidden="true" />
          {stateCopy(keyData, t)}
        </span>
      </td>
      <td data-label={t('admin.quota.reset-time')}>
        <span data-testid="admin-quota-cooldown">{reset}</span>
      </td>
    </tr>
  );
}

export function QuotaPage() {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const query = useAdminQuota(true);
  const [now, setNow] = useState(() => Date.now());
  const hasNoSnapshot = query.data === undefined;
  const fetching = query.fetchStatus === 'fetching';
  const loading = useLoadingTiming(hasNoSnapshot && fetching);
  const waitingForLive =
    hasNoSnapshot && (query.fetchStatus === 'paused' || query.fetchStatus === 'idle');
  const forbidden = (query.error as { status?: number } | null)?.status === 403;
  const stale = query.data !== undefined && (query.isError || now - query.dataUpdatedAt >= 20_000);

  useEffect(() => {
    document.title = t('shell.document-title', { view: t('admin.quota.title') });
  }, [t]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const heading = (
    <header className={styles.heading}>
      <div>
        <h1 tabIndex={-1}>{t('admin.quota.title')}</h1>
        <p>{t('admin.quota.summary')}</p>
        {query.dataUpdatedAt > 0 ? (
          <p className={styles.updated} data-testid="admin-quota-updated">
            {t('admin.quota.updated', {
              time: new Intl.DateTimeFormat(localeCode(locale), {
                dateStyle: 'medium',
                timeStyle: 'short',
              }).format(query.dataUpdatedAt),
            })}
          </p>
        ) : null}
      </div>
      <button
        className={styles.refresh}
        type="button"
        onClick={() => void query.refetch()}
        disabled={query.isFetching}
        aria-busy={query.isFetching}
        data-testid="admin-quota-refresh"
      >
        <Icon name="refresh" size={16} />
        {t('admin.quota.refresh')}
      </button>
    </header>
  );

  if (forbidden) {
    return (
      <div data-testid="admin-page">
        <div data-testid="admin-quota-page">
          <ForbiddenState
            testId="admin-quota-forbidden"
            ctaTestId="admin-quota-forbidden-home"
            onHome={() => void navigate('/')}
          />
        </div>
      </div>
    );
  }

  return (
    <div data-testid="admin-page">
      <div className={styles.page} data-testid="admin-quota-page">
        {heading}
        {stale ? (
          <div className={styles.stale} role="status" data-testid="admin-quota-stale">
            {t('admin.quota.stale', {
              age: formatAge(Math.max(0, now - query.dataUpdatedAt), locale),
            })}
            {query.isError ? (
              <button
                type="button"
                onClick={() => void query.refetch()}
                data-testid="admin-quota-stale-retry"
              >
                {t('admin.quota.retry')}
              </button>
            ) : null}
          </div>
        ) : null}
        {query.data === undefined && query.isError ? (
          <div data-testid="admin-quota-error">
            <div className={styles.error} role="alert">
              <div>
                <strong>{t('admin.quota.error-title')}</strong>
                <p>{t('admin.quota.error-body')}</p>
              </div>
              <button
                className={styles.refresh}
                type="button"
                onClick={() => void query.refetch()}
                data-testid="admin-quota-retry"
              >
                {t('admin.quota.retry')}
              </button>
            </div>
            <div className={styles.skeleton} aria-hidden="true" />
          </div>
        ) : waitingForLive ? (
          <div className={styles.waiting} role="status" data-testid="admin-quota-waiting">
            <div>
              <strong>{t('admin.quota.waiting-title')}</strong>
              <p>{t('admin.quota.waiting-body')}</p>
            </div>
            <button
              className={styles.refresh}
              type="button"
              onClick={() => void query.loadOnce().catch(() => undefined)}
              disabled={query.isFetching}
              aria-busy={query.isFetching}
              data-testid="admin-quota-waiting-load-once"
            >
              {t('admin.quota.load-once')}
            </button>
          </div>
        ) : loading ? (
          <div className={styles.loading} aria-busy="true" data-testid="admin-quota-loading">
            <p role="status">{t('admin.quota.loading')}</p>
            <div className={styles.skeleton} aria-hidden="true" />
          </div>
        ) : query.data === undefined ? (
          <div aria-busy="true" />
        ) : query.data.keys.length === 0 ? (
          <EmptyState
            testId="admin-quota"
            icon="key"
            title={t('admin.quota.empty-title')}
            text={t('admin.quota.empty-body')}
            action={
              <Link to="/keys" className={styles.link} data-testid="admin-quota-empty-cta">
                {t('admin.quota.empty-cta')}
              </Link>
            }
          />
        ) : (
          <>
            {query.isFetching && query.data !== undefined ? (
              <p className={styles.refreshing} role="status">
                {t('admin.quota.refreshing')}
              </p>
            ) : null}
            <section className={styles.panel} aria-labelledby="admin-quota-keys-heading">
              <div className={styles.panelHeading}>
                <h2 id="admin-quota-keys-heading">{t('admin.quota.keys')}</h2>
                <span className={styles.count}>{formatNumber(query.data.keys.length, locale)}</span>
              </div>
              <div className={styles.tableWrap}>
                <table data-testid="admin-quota-key-table">
                  <caption>{t('admin.quota.key-table-caption')}</caption>
                  <thead>
                    <tr>
                      <th scope="col">{t('admin.quota.key')}</th>
                      <th scope="col">{t('admin.quota.owner')}</th>
                      <th scope="col">{t('admin.quota.node')}</th>
                      <th scope="col">{t('admin.quota.used')}</th>
                      <th scope="col">{t('admin.quota.state')}</th>
                      <th scope="col">{t('admin.quota.reset-time')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {query.data.keys.map((keyData) => (
                      <QuotaRow key={keyData.key_id} keyData={keyData} now={now} locale={locale} />
                    ))}
                  </tbody>
                </table>
              </div>
              <p className={styles.note}>{t('admin.quota.lane-data-unavailable')}</p>
            </section>
            <section className={styles.panel} aria-labelledby="admin-quota-nodes-heading">
              <div className={styles.panelHeading}>
                <h2 id="admin-quota-nodes-heading">{t('admin.quota.nodes')}</h2>
              </div>
              <p className={styles.note} data-testid="admin-quota-node-table">
                {t('admin.quota.node-data-unavailable')}
              </p>
              <div className={styles.history}>
                <div>
                  <h3>{t('admin.quota.hourly-history')}</h3>
                  <p>{t('admin.quota.history-unavailable')}</p>
                </div>
                <div>
                  <h3>{t('admin.quota.burst-history')}</h3>
                  <p>{t('admin.quota.history-unavailable')}</p>
                </div>
              </div>
            </section>
          </>
        )}
      </div>
    </div>
  );
}
