import type { RefObject } from 'react';
import { useI18n } from '../../shared';
import styles from './SetupPage.module.css';
import { bannerCopy } from './setupModel';
import type { KeyBanner } from './setupModel';

type KeyBannerProps = {
  banner: KeyBanner;
  bannerRef: RefObject<HTMLDivElement | null>;
  onRetry: () => void;
};

/** The add-key failure banner; a retry is offered except when the key itself is invalid. */
export function KeyBannerView({ banner, bannerRef, onRetry }: KeyBannerProps) {
  const { t } = useI18n();
  return (
    <div
      ref={bannerRef}
      tabIndex={-1}
      role="alert"
      className={styles.error}
      data-testid="wizard-key-error"
      data-setup-focus="true"
    >
      {t(bannerCopy[banner])}
      {banner !== 'invalid' ? (
        <button type="button" className={styles.ghost} onClick={onRetry}>
          {t('common.retry')}
        </button>
      ) : null}
    </div>
  );
}
