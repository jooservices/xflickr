import { Icon, useI18n } from '../../shared';
import type { MessageKey } from '../../shared/i18n';
import type { FlickrAccount } from '../../shared/api/gen/models';
import styles from './keys.module.css';

const statusCopy: Record<FlickrAccount['status'], MessageKey> = {
  ok: 'keys.accounts.ok',
  reconnect_required: 'keys.accounts.reconnect-required',
  revoked: 'keys.accounts.revoked',
};

type AccountsSectionProps = {
  keyId: number;
  /** `undefined` while the list loads. */
  accounts: FlickrAccount[] | undefined;
  failed: boolean;
  connecting: boolean;
  onConnect: (keyId: number) => void;
  onRetry: () => void;
};

/** Flickr accounts authorized through one key, with Reconnect for those that need it. */
export function AccountsSection({
  keyId,
  accounts,
  failed,
  connecting,
  onConnect,
  onRetry,
}: AccountsSectionProps) {
  const { t } = useI18n();
  const rows = accounts?.filter((account) => account.key_id === keyId);

  let body;
  if (failed && rows === undefined) {
    body = (
      <p className={styles.muted}>
        {t('keys.accounts.error')}{' '}
        <button
          type="button"
          className={styles.link}
          onClick={onRetry}
          data-testid="keys-accounts-retry"
        >
          {t('common.retry')}
        </button>
      </p>
    );
  } else if (rows === undefined) {
    body = (
      <div aria-hidden="true">
        <div className={styles.shimmer} />
        <div className={styles.shimmer} />
      </div>
    );
  } else if (rows.length === 0) {
    body = (
      <>
        <p className={styles.muted} data-testid="keys-accounts-none">
          {t('keys.accounts.none')}
        </p>
        <button
          type="button"
          className={styles.ghostSmall}
          disabled={connecting}
          onClick={() => onConnect(keyId)}
          data-testid={`keys-accounts-connect-${String(keyId)}`}
        >
          {t('keys.accounts.connect')}
        </button>
      </>
    );
  } else {
    body = (
      <ul className={styles.accountList}>
        {rows.map((account) => (
          <li
            key={account.id}
            className={styles.account}
            data-testid={`keys-account-${account.nsid}`}
          >
            <span
              className={account.status === 'ok' ? styles.dotOk : styles.dotBad}
              aria-hidden="true"
            >
              <Icon name={account.status === 'ok' ? 'check' : 'alert'} size={13} />
            </span>
            <span className={styles.accountName}>
              <strong>{account.username}</strong>
              <code>{account.nsid}</code>
            </span>
            <span className={styles.accountSide}>
              <span
                className={`${styles.pill} ${account.status === 'ok' ? styles.toneOk : styles.toneBad}`}
                data-testid={`keys-account-status-${account.nsid}`}
              >
                {t(statusCopy[account.status])}
              </span>
              {account.status === 'ok' ? null : (
                <button
                  type="button"
                  className={styles.ghostSmall}
                  disabled={connecting}
                  onClick={() => onConnect(keyId)}
                  data-testid={`keys-account-reconnect-${account.nsid}`}
                >
                  {t('keys.accounts.reconnect')}
                </button>
              )}
            </span>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <section aria-label={t('keys.accounts.title')} data-testid={`keys-accounts-${String(keyId)}`}>
      <h4 className={styles.sectionTitle}>{t('keys.accounts.title')}</h4>
      {body}
    </section>
  );
}
