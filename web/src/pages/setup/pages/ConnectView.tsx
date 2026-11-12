import type { ReactNode } from 'react';
import { Icon, useI18n } from '../../../shared';
import type { ApiKey } from '../../../shared/api/gen/models';
import { connectErrorCopy } from '../setupModel';
import type { ConnectError } from '../setupModel';
import styles from '../SetupPage.module.css';

type ConnectViewProps = {
  activeKey: ApiKey | null;
  validKeys: ApiKey[];
  error: ConnectError | null;
  waiting: boolean;
  rollback?: ReactNode;
  setSelectedKeyId: (id: number) => void;
  authorize: () => void;
  cancel: () => void;
};

/** Presents the Flickr permission screen and the connect actions. */
export function ConnectView(props: ConnectViewProps) {
  const { t } = useI18n();
  return (
    <div className={styles.step} data-testid="wizard-step-connect">
      <p className={styles.lead}>{t('setup.connect.lead')}</p>
      {props.error ? <ConnectErrorView error={props.error} retry={props.authorize} /> : null}
      <ConnectConsent />
      <KeySelect
        activeKey={props.activeKey}
        validKeys={props.validKeys}
        setSelectedKeyId={props.setSelectedKeyId}
      />
      <p id="wizard-leave" className={styles.note}>
        {t('setup.connect.leave')}
      </p>
      {props.waiting ? <WaitingStatus /> : null}
      <ConnectActions {...props} />
    </div>
  );
}

function ConnectConsent() {
  const { t } = useI18n();
  return (
    <div className={styles.consent} data-testid="wizard-connect-perms">
      <b>
        <Icon name="lock" /> {'flickr.com'}
      </b>
      <h2>{t('setup.connect.perm.title')}</h2>
      <ul>
        <li>{t('setup.connect.perm.read')}</li>
        <li>{t('setup.connect.perm.private')}</li>
      </ul>
      <p>{t('setup.connect.perm.none')}</p>
    </div>
  );
}

function KeySelect({
  activeKey,
  validKeys,
  setSelectedKeyId,
}: Pick<ConnectViewProps, 'activeKey' | 'validKeys' | 'setSelectedKeyId'>) {
  const { t } = useI18n();
  if (validKeys.length < 2) return null;
  return (
    <label>
      {t('setup.connect.key-select')}
      <select
        value={activeKey?.id ?? ''}
        onChange={(event) => setSelectedKeyId(Number(event.target.value))}
        data-testid="wizard-key-select"
      >
        {validKeys.map((key) => (
          <option key={key.id} value={key.id}>
            {key.label}
          </option>
        ))}
      </select>
    </label>
  );
}

function WaitingStatus() {
  const { t } = useI18n();
  return (
    <p role="status" className={styles.note} data-testid="wizard-connect-waiting">
      {t('setup.connect.waiting')}
    </p>
  );
}

function ConnectErrorView({ error, retry }: { error: ConnectError; retry: () => void }) {
  const { t } = useI18n();
  const testId =
    error === 'denied'
      ? 'wizard-connect-denied'
      : error === 'expired'
        ? 'wizard-connect-expired'
        : 'wizard-connect-error';
  return (
    <div role="alert" className={styles.error} data-testid={testId}>
      {t(connectErrorCopy[error])}{' '}
      <button className={styles.ghost} onClick={retry} data-testid="wizard-connect-retry">
        {t('common.retry')}
      </button>
    </div>
  );
}

function ConnectActions({ activeKey, waiting, authorize, cancel, rollback }: ConnectViewProps) {
  const { t } = useI18n();
  return (
    <div className={styles.actions}>
      <button
        className={styles.primary}
        disabled={waiting || !activeKey}
        aria-describedby="wizard-leave"
        onClick={authorize}
        data-testid="wizard-connect-allow"
      >
        {t('setup.connect.allow')}
      </button>
      {waiting ? (
        <button className={styles.ghost} onClick={cancel} data-testid="wizard-connect-cancel">
          {t('setup.connect.cancel')}
        </button>
      ) : (
        rollback
      )}
    </div>
  );
}
