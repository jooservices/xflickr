import { useEffect, useState } from 'react';
import type { JSX } from 'react';
import { Link } from 'react-router';
import { useI18n } from '../../../shared';
import { ForbiddenOverview } from './ForbiddenOverview';
import styles from '../quota/QuotaPage.module.css';
import { OverviewHeader } from './OverviewHeader';
import { OverviewKpis } from './OverviewKpis';
import { OverviewNotices } from './OverviewNotices';
import { useOverviewData } from './useOverviewData';

/** First slice of the ADMIN overview: the key quota numbers that an existing endpoint already serves. */
export function OverviewPage(): JSX.Element {
  const { t } = useI18n();
  const [now, setNow] = useState(() => Date.now());
  const data = useOverviewData(now);

  useEffect(() => {
    document.title = t('shell.document-title', { view: t('admin.overview.title') });
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [t]);

  if (data.forbidden) return <ForbiddenOverview />;
  return (
    <div data-testid="admin-page">
      <div className={styles.page} data-testid="admin-overview-page">
        <OverviewHeader refreshing={data.refreshing} onRefresh={data.refetch} />
        <OverviewNotices failed={data.failed} stale={data.stale} />
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
