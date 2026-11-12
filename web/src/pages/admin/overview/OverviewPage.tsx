import { useEffect, useState } from 'react';
import type { JSX } from 'react';
import { Link, useNavigate } from 'react-router';
import { ForbiddenState, useI18n } from '../../../shared';
import styles from '../quota/QuotaPage.module.css';
import { OverviewHeader } from './OverviewHeader';
import { OverviewKpis } from './OverviewKpis';
import { useOverviewData } from './useOverviewData';

/** First slice of the ADMIN overview: the key quota numbers that an existing endpoint already serves. */
export function OverviewPage(): JSX.Element {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [now, setNow] = useState(() => Date.now());
  const data = useOverviewData(now);

  useEffect(() => {
    document.title = t('shell.document-title', { view: t('admin.overview.title') });
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [t]);

  if (data.forbidden) {
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
  return (
    <div data-testid="admin-page">
      <div className={styles.page} data-testid="admin-overview-page">
        <OverviewHeader refreshing={data.refreshing} onRefresh={data.refetch} />
        {data.failed ? (
          <div className={styles.error} role="alert" data-testid="admin-overview-error">
            <strong>{t('admin.overview.error')}</strong>
          </div>
        ) : null}
        {data.stale ? (
          <div className={styles.stale} role="status" data-testid="admin-overview-stale">
            {t('admin.overview.stale')}
          </div>
        ) : null}
        <OverviewKpis exhausted={data.exhausted} keyCount={data.keyCount} />
        <nav aria-label={t('admin.overview.links')}>
          <Link to="/admin/quota" data-testid="admin-overview-link-quota">
            {t('admin.overview.link-quota')}
          </Link>
        </nav>
        <p role="note" data-testid="admin-overview-pending-sections">
          {t('admin.overview.pending-sections')}
        </p>
      </div>
    </div>
  );
}
