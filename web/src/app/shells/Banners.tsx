import { useState } from 'react';
import { Link } from 'react-router';
import { Icon, useI18n } from '../../shared';
import type { Attention } from './data';
import styles from './shells.module.css';

const MAX_BANNERS = 2;

type BannersProps = {
  offline: boolean;
  crawlingOff: boolean;
  attention: Attention[];
  onReconnect: (item: Attention) => void;
};

/**
 * Banner order in main: offline, crawling off (no valid key), key revoked, token
 * expired (at most 2, then "N more"). Offline always stays first.
 */
export function Banners({ offline, crawlingOff, attention, onReconnect }: BannersProps) {
  const { t } = useI18n();
  const [dismissed, setDismissed] = useState<ReadonlySet<string>>(new Set());
  const visible = (kind: Attention['kind'][]) =>
    attention.filter((item) => kind.includes(item.kind) && !dismissed.has(item.id));
  const keys = visible(['key_revoked']);
  const tokens = visible(['token_expired', 'cloud_expired']);
  const shownTokens = tokens.slice(0, MAX_BANNERS);

  const dismiss = (id: string) => {
    setDismissed(new Set([...dismissed, id]));
  };

  return (
    <>
      {offline ? (
        <div className={styles.banner} role="status" data-testid="shell-offline-banner">
          <span>
            <Icon name="info" /> {t('shell.offline.banner')}
          </span>
        </div>
      ) : null}
      {crawlingOff ? (
        <div
          className={`${styles.banner} ${styles.warn}`}
          role="status"
          data-testid="shell-crawling-off-banner"
        >
          <span>
            <Icon name="alert" /> {t('shell.banner.crawling-off')}
          </span>
          <Link className={styles.btn} to="/onboarding" data-testid="shell-banner-add-key">
            {t('shell.banner.setup')}
          </Link>
        </div>
      ) : null}
      {keys.map((item) => (
        <div
          key={item.id}
          className={`${styles.banner} ${styles.warn}`}
          role="alert"
          data-testid={`shell-key-revoked-banner-${item.id}`}
        >
          <span>
            <Icon name="alert" /> {t('shell.banner.key-revoked', { label: item.label })}
          </span>
          <span className={styles.bannerActions}>
            <Link
              className={styles.btn}
              to="/keys"
              data-testid={`shell-banner-reconnect-${item.id}`}
            >
              {t('shell.banner.reconnect')}
            </Link>
            <button
              type="button"
              className={styles.btn}
              onClick={() => {
                dismiss(item.id);
              }}
              data-testid={`shell-banner-dismiss-${item.id}`}
            >
              {t('shell.banner.dismiss')}
            </button>
          </span>
        </div>
      ))}
      {shownTokens.map((item) => (
        <div
          key={item.id}
          className={`${styles.banner} ${styles.warn}`}
          role="alert"
          data-testid={`shell-token-expired-banner-${item.id}`}
        >
          <span>
            <Icon name="alert" />{' '}
            {t(
              item.kind === 'cloud_expired'
                ? 'shell.banner.cloud-expired'
                : 'shell.banner.token-expired',
              { name: item.label },
            )}
          </span>
          <button
            type="button"
            className={styles.btn}
            onClick={() => {
              onReconnect(item);
            }}
            data-testid={`shell-banner-reconnect-${item.id}`}
          >
            {t('shell.banner.reconnect')}
          </button>
        </div>
      ))}
      {tokens.length > MAX_BANNERS ? (
        <Link to="/accounts">{t('shell.banner.more', { count: tokens.length - MAX_BANNERS })}</Link>
      ) : null}
    </>
  );
}
