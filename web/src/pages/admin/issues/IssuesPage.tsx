import { useEffect } from 'react';
import { useNavigate } from 'react-router';
import type { AdminOpenIncident, AdminOpenIncidentSeverity } from '../../../shared/api/gen/models';
import { EmptyState, ErrorState, LoadingState, useI18n } from '../../../shared';
import type { Locale } from '../../../shared';
import { useAdminOpenIncidents } from '../../../features/admin/issues';
import styles from './IssuesPage.module.css';

function localeCode(locale: Locale): string {
  return locale === 'vi' ? 'vi-VN' : 'en-US';
}

function severityLabel(severity: AdminOpenIncidentSeverity, t: ReturnType<typeof useI18n>['t']) {
  switch (severity) {
    case 'info':
      return t('admin.issues.severity-info');
    case 'warning':
      return t('admin.issues.severity-warning');
    case 'critical':
      return t('admin.issues.severity-critical');
  }
}

function formatTimestamp(value: string, locale: Locale): string {
  return new Intl.DateTimeFormat(localeCode(locale), {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: 'UTC',
  }).format(new Date(value));
}

function IncidentRows({ items, locale }: { items: AdminOpenIncident[]; locale: Locale }) {
  const { t } = useI18n();

  return (
    <div className={styles.tableWrap} data-testid="admin-issues-table">
      <table>
        <caption>{t('admin.issues.table-caption')}</caption>
        <thead>
          <tr>
            <th scope="col">{t('admin.issues.column-severity')}</th>
            <th scope="col">{t('admin.issues.column-incident')}</th>
            <th scope="col">{t('admin.issues.column-first-seen')}</th>
            <th scope="col">{t('admin.issues.column-last-seen')}</th>
            <th scope="col">{t('admin.issues.column-count')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((incident) => (
            <tr key={incident.id} data-testid={`admin-issues-row-${incident.id}`}>
              <td data-label={t('admin.issues.column-severity')}>
                <span className={`${styles.severity} ${styles[incident.severity]}`}>
                  <span aria-hidden="true" />
                  {severityLabel(incident.severity, t)}
                </span>
              </td>
              <td data-label={t('admin.issues.column-incident')}>
                {t('admin.issues.incident-id', { id: String(incident.id) })}
              </td>
              <td data-label={t('admin.issues.column-first-seen')}>
                <time dateTime={incident.first_seen} title={incident.first_seen}>
                  {formatTimestamp(incident.first_seen, locale)}
                </time>
              </td>
              <td data-label={t('admin.issues.column-last-seen')}>
                <time dateTime={incident.last_seen} title={incident.last_seen}>
                  {formatTimestamp(incident.last_seen, locale)}
                </time>
              </td>
              <td data-label={t('admin.issues.column-count')}>{incident.count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function IssuesPage() {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const query = useAdminOpenIncidents();
  const errorStatus = (query.error as (Error & { status?: number }) | null)?.status;
  const forbidden = errorStatus === 403;
  const items = query.data?.pages.flatMap((page) => page.items) ?? [];
  const updatedAt = query.data?.pages.at(-1)?.server_time;

  useEffect(() => {
    document.title = t('shell.document-title', { view: t('admin.issues.title') });
  }, [t]);

  if (forbidden) {
    return (
      <section className={styles.forbidden} role="alert" data-testid="admin-issues-forbidden">
        <h1>{t('admin.issues.forbidden-title')}</h1>
        <p>{t('admin.issues.forbidden-body')}</p>
        <button type="button" onClick={() => void navigate('/')}>
          {t('admin.issues.forbidden-home')}
        </button>
      </section>
    );
  }

  return (
    <section className={styles.page} data-testid="admin-issues-page">
      <header className={styles.heading}>
        <div>
          <h1 tabIndex={-1}>{t('admin.issues.title')}</h1>
          <p>{t('admin.issues.summary')}</p>
          {updatedAt ? (
            <p className={styles.updated}>
              {t('admin.issues.updated', { time: formatTimestamp(updatedAt, locale) })}
            </p>
          ) : null}
        </div>
        <button
          className={styles.refresh}
          type="button"
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
          aria-busy={query.isFetching}
          data-testid="admin-issues-refresh"
        >
          {query.isFetching ? t('admin.issues.refreshing') : t('admin.issues.refresh')}
        </button>
      </header>

      {query.isPending ? (
        <LoadingState testId="admin-issues" label={t('admin.issues.loading')} tiles={4} />
      ) : null}

      {query.isError && items.length === 0 ? (
        <ErrorState
          testId="admin-issues"
          title={t('admin.issues.error-title')}
          text={t('admin.issues.error-body')}
          onRetry={() => void query.refetch()}
        />
      ) : null}

      {query.isError && items.length > 0 ? (
        <div className={styles.stale} role="status" data-testid="admin-issues-stale">
          {t('admin.issues.stale')}
        </div>
      ) : null}

      {!query.isPending && !query.isError && items.length === 0 ? (
        <EmptyState
          testId="admin-issues"
          icon="flag"
          title={t('admin.issues.empty-title')}
          text={t('admin.issues.empty-body')}
        />
      ) : null}

      {items.length > 0 ? <IncidentRows items={items} locale={locale} /> : null}

      {query.hasNextPage ? (
        <button
          className={styles.loadMore}
          type="button"
          onClick={() => void query.fetchNextPage()}
          disabled={query.isFetchingNextPage}
          data-testid="admin-issues-load-more"
        >
          {query.isFetchingNextPage ? t('admin.issues.loading-more') : t('admin.issues.load-more')}
        </button>
      ) : null}
    </section>
  );
}
