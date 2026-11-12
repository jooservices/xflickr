import { useEffect } from 'react';
import type { JSX } from 'react';
import { Link, useNavigate } from 'react-router';
import { useQuery } from '@tanstack/react-query';
import { getAdminQueue } from '../../../shared/api/gen/admin/admin';
import type { AdminQueue } from '../../../shared/api/gen/models';
import { ForbiddenState, Icon, useI18n } from '../../../shared';
import { useAdminQuota } from '../../../features/admin/quota';
import styles from '../quota/QuotaPage.module.css';

const QUEUE_QUERY_KEY = ['admin', 'queue'] as const;
const QUEUE_POLL_MS = 30_000;

async function loadQueue(signal: AbortSignal): Promise<AdminQueue> {
  const response = await getAdminQueue({ signal });
  if (response.status !== 200) {
    throw Object.assign(new Error('Could not load the queue'), { status: response.status });
  }
  return response.data;
}

function statusOf(error: unknown): number | undefined {
  return (error as { status?: number } | null)?.status;
}

/** First slice of the ADMIN overview: key quota and queue numbers that existing endpoints already serve. */
export function OverviewPage(): JSX.Element {
  const { locale, t } = useI18n();
  const navigate = useNavigate();
  const quota = useAdminQuota(true);
  const queue = useQuery({
    queryKey: QUEUE_QUERY_KEY,
    queryFn: ({ signal }) => loadQueue(signal),
    retry: false,
    refetchInterval: (query) => (statusOf(query.state.error) === 403 ? false : QUEUE_POLL_MS),
    refetchIntervalInBackground: false,
  });
  const forbidden = statusOf(quota.error) === 403 || statusOf(queue.error) === 403;
  const number = new Intl.NumberFormat(locale);

  useEffect(() => {
    document.title = t('shell.document-title', { view: t('admin.overview.title') });
  }, [t]);

  if (forbidden) {
    return (
      <div data-testid="admin-page">
        <div data-testid="admin-overview-page">
          <ForbiddenState
            testId="admin-overview-forbidden"
            ctaTestId="admin-overview-forbidden-home"
            onHome={() => void navigate('/')}
          />
        </div>
      </div>
    );
  }

  const keys = quota.data?.keys;
  const exhausted = keys?.filter((key) => key.state === 'exhausted').length;
  const backlog = queue.data?.backlog_by_plan.reduce((sum, item) => sum + item.backlog_calls, 0);
  const ready = queue.data?.ready_queue_sizes.reduce((sum, item) => sum + item.ready_jobs, 0);
  const age = queue.data?.oldest_item_age_seconds;
  const refreshing = quota.isFetching || queue.isFetching;
  const failed =
    (quota.isError && keys === undefined) || (queue.isError && queue.data === undefined);
  const value = (n: number | undefined) => (n === undefined ? '—' : number.format(n));

  return (
    <div data-testid="admin-page">
      <div className={styles.page} data-testid="admin-overview-page">
        <header className={styles.heading}>
          <div>
            <h1 tabIndex={-1}>{t('admin.overview.title')}</h1>
            <p>{t('admin.overview.subtitle')}</p>
          </div>
          <button
            className={styles.refresh}
            type="button"
            disabled={refreshing}
            aria-busy={refreshing}
            onClick={() => {
              void quota.refetch();
              void queue.refetch();
            }}
            data-testid="admin-overview-refresh"
          >
            <Icon name="refresh" size={16} />
            {t('admin.overview.refresh')}
          </button>
        </header>

        {failed ? (
          <div className={styles.error} role="alert" data-testid="admin-overview-error">
            <strong>{t('admin.overview.error')}</strong>
          </div>
        ) : null}

        <section aria-label={t('admin.overview.kpis')} data-testid="admin-overview-kpis">
          <dl>
            <div data-testid="admin-overview-keys-at-quota">
              <dt>{t('admin.overview.keys-at-quota')}</dt>
              <dd>
                {value(exhausted)}
                {keys === undefined ? null : (
                  <small>
                    {t('admin.overview.of-keys', { count: number.format(keys.length) })}
                  </small>
                )}
              </dd>
            </div>
            <div data-testid="admin-overview-oldest-age">
              <dt>{t('admin.overview.oldest-age')}</dt>
              <dd>
                {age === undefined
                  ? '—'
                  : t('admin.overview.seconds', { count: number.format(age) })}
              </dd>
            </div>
            <div data-testid="admin-overview-backlog">
              <dt>{t('admin.overview.backlog')}</dt>
              <dd>{value(backlog)}</dd>
            </div>
            <div data-testid="admin-overview-ready">
              <dt>{t('admin.overview.ready')}</dt>
              <dd>{value(ready)}</dd>
            </div>
          </dl>
        </section>

        <nav aria-label={t('admin.overview.links')}>
          <Link to="/admin/quota" data-testid="admin-overview-link-quota">
            {t('admin.overview.link-quota')}
          </Link>{' '}
          <Link to="/admin/pipeline" data-testid="admin-overview-link-pipeline">
            {t('admin.overview.link-pipeline')}
          </Link>
        </nav>

        <p role="note" data-testid="admin-overview-pending-sections">
          {t('admin.overview.pending-sections')}
        </p>
      </div>
    </div>
  );
}
