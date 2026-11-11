import { useEffect, useState } from 'react';
import type { JSX } from 'react';
import { useNavigate } from 'react-router';
import type { AdminKeyQuota } from '../../../shared/api/gen/models';
import { EmptyState, ForbiddenState, Icon, useI18n, useLoadingTiming } from '../../../shared';
import type { MessageKey } from '../../../shared';
import { useAdminQuota } from '../../../features/admin/quota';
import styles from '../quota/QuotaPage.module.css';

function stateLabel(state: AdminKeyQuota['state'], t: (key: MessageKey) => string): string {
  switch (state) {
    case 'ok':
      return t('admin.quota.status-ok');
    case 'cooling':
      return t('admin.quota.status-cooling');
    case 'exhausted':
      return t('admin.quota.status-exhausted');
  }
}

export function NodesPage(): JSX.Element {
  const { t } = useI18n();
  const navigate = useNavigate();
  const query = useAdminQuota(true);
  const [now, setNow] = useState(() => Date.now());
  const hasNoSnapshot = query.data === undefined;
  const loading = useLoadingTiming(hasNoSnapshot && query.fetchStatus === 'fetching');
  const waitingForLive =
    hasNoSnapshot && (query.fetchStatus === 'paused' || query.fetchStatus === 'idle');
  const forbidden = (query.error as { status?: number } | null)?.status === 403;
  const stale = query.data !== undefined && (query.isError || now - query.dataUpdatedAt >= 20_000);

  useEffect(() => {
    document.title = t('shell.document-title', { view: t('admin.nodes.title') });
  }, [t]);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  if (forbidden) {
    return (
      <div data-testid="admin-page">
        <div data-testid="admin-nodes-page">
          <ForbiddenState
            testId="admin-nodes-forbidden"
            ctaTestId="admin-nodes-forbidden-home"
            onHome={() => void navigate('/')}
          />
        </div>
      </div>
    );
  }

  return (
    <div data-testid="admin-page">
      <div className={styles.page} data-testid="admin-nodes-page">
        <header className={styles.heading}>
          <div>
            <h1 tabIndex={-1}>{t('admin.nodes.title')}</h1>
            <p>{t('admin.nodes.subtitle')}</p>
          </div>
          <button
            className={styles.refresh}
            type="button"
            onClick={() => void query.refetch()}
            disabled={query.isFetching}
            aria-busy={query.isFetching}
            data-testid="admin-nodes-refresh"
          >
            <Icon name="refresh" size={16} />
            {t('admin.nodes.refresh')}
          </button>
        </header>

        {stale ? (
          <div className={styles.stale} role="status" data-testid="admin-nodes-stale">
            {t('admin.nodes.stale')}
            {query.isError ? (
              <button
                type="button"
                onClick={() => void query.refetch()}
                data-testid="admin-nodes-error-retry"
              >
                {t('admin.nodes.retry')}
              </button>
            ) : null}
          </div>
        ) : null}

        {hasNoSnapshot && query.isError ? (
          <div data-testid="admin-nodes-error">
            <div className={styles.error} role="alert">
              <div>
                <strong>{t('admin.nodes.error')}</strong>
              </div>
              <button
                className={styles.refresh}
                type="button"
                onClick={() => void query.loadOnce().catch(() => undefined)}
                data-testid="admin-nodes-error-retry"
              >
                {t('admin.nodes.retry')}
              </button>
            </div>
            <div className={styles.skeleton} aria-hidden="true" />
          </div>
        ) : waitingForLive ? (
          <div className={styles.waiting} role="status">
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
            >
              {t('admin.quota.load-once')}
            </button>
          </div>
        ) : loading ? (
          <div className={styles.loading} aria-busy="true" data-testid="admin-nodes-loading">
            <p role="status">{t('admin.nodes.loading')}</p>
            <div className={styles.skeleton} aria-hidden="true" />
          </div>
        ) : query.data === undefined ? (
          <div aria-busy="true" />
        ) : (
          <>
            {query.isFetching ? (
              <p className={styles.refreshing} role="status">
                {t('admin.quota.refreshing')}
              </p>
            ) : null}
            <section
              className={styles.panel}
              aria-labelledby="admin-nodes-key-heading"
              data-testid="admin-nodes-key-placement"
            >
              <div className={styles.panelHeading}>
                <h2 id="admin-nodes-key-heading">{t('admin.nodes.keys-title')}</h2>
              </div>
              {query.data.keys.length === 0 ? (
                <EmptyState testId="admin-nodes" icon="key" title={t('admin.nodes.empty')} />
              ) : (
                <div className={styles.tableWrap}>
                  <table>
                    <caption>{t('admin.nodes.keys-title')}</caption>
                    <thead>
                      <tr>
                        <th scope="col">{t('admin.nodes.key')}</th>
                        <th scope="col">{t('admin.nodes.user')}</th>
                        <th scope="col">{t('admin.nodes.state')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {query.data.keys.map((key) => (
                        <tr key={key.key_id}>
                          <td data-label={t('admin.nodes.key')} className={styles.keyCell}>
                            <span>{key.label}</span>
                            <code>#{key.key_id}</code>
                          </td>
                          <td data-label={t('admin.nodes.user')}>
                            {key.user_id === undefined
                              ? '—'
                              : t('admin.quota.user-id', { id: String(key.user_id) })}
                          </td>
                          <td data-label={t('admin.nodes.state')}>
                            <span className={`${styles.state} ${styles[key.state]}`}>
                              <span aria-hidden="true" />
                              {stateLabel(key.state, t)}
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </section>
          </>
        )}
      </div>
    </div>
  );
}
