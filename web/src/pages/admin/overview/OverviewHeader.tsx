import type { JSX } from 'react';
import { Icon, useI18n } from '../../../shared';
import styles from '../quota/QuotaPage.module.css';

type OverviewHeaderProps = { refreshing: boolean; onRefresh: () => void };

/** Title, subtitle and the refresh button of the overview. */
export function OverviewHeader({ refreshing, onRefresh }: OverviewHeaderProps): JSX.Element {
  const { t } = useI18n();
  return (
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
        onClick={onRefresh}
        data-testid="admin-overview-refresh"
      >
        <Icon name="refresh" size={16} />
        {t('admin.overview.refresh')}
      </button>
    </header>
  );
}
