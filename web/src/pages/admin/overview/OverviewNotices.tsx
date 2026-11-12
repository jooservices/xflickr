import type { JSX } from 'react';
import { useI18n } from '../../../shared';
import styles from '../quota/QuotaPage.module.css';

type OverviewNoticesProps = { failed: boolean; stale: boolean };

/** The error alert (nothing loaded) and the stale notice (last numbers kept) of the overview. */
export function OverviewNotices({ failed, stale }: OverviewNoticesProps): JSX.Element {
  const { t } = useI18n();
  return (
    <>
      {failed ? (
        <div className={styles.error} role="alert" data-testid="admin-overview-error">
          <strong>{t('admin.overview.error')}</strong>
        </div>
      ) : null}
      {stale ? (
        <div className={styles.stale} role="status" data-testid="admin-overview-stale">
          {t('admin.overview.stale')}
        </div>
      ) : null}
    </>
  );
}
