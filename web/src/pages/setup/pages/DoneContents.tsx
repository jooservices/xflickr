import type { ReactNode } from 'react';
import type { RefObject } from 'react';
import type { ApiKey, FlickrAccount } from '../../../shared/api/gen/models';
import { Icon, useI18n } from '../../../shared';
import { accountStatusCopy } from '../setupModel';
import type { SetupData } from '../useSetupData';
import type { SetupPageComponentProps } from '../types';
import styles from '../SetupPage.module.css';

type DoneSettings = {
  query: { isError: boolean; refetch: () => Promise<unknown> };
  depth: number;
  depthMax: number;
  setDepthChoice: (depth: number | null) => void;
};

type DoneActions = {
  finishError: 'finish' | 'depth' | null;
  starting: boolean;
  finish: (depth: number) => Promise<void>;
  goToConnect: () => void;
  setAddKeyOpen: (open: boolean) => void;
};

type DoneContentsProps = {
  data: SetupData;
  settings: DoneSettings;
  actions: DoneActions;
  rollback?: ReactNode;
  rows: RefObject<Map<number, HTMLLIElement>>;
  focusAccountId: number | undefined;
  showSettingsWarning: boolean;
};

/** Renders the account list, depth control and crawl actions for the done page. */
export function DoneContents(props: DoneContentsProps) {
  const { t } = useI18n();
  const canFinish = props.data.accounts.some((account) => account.status === 'ok');
  return (
    <div className={styles.step} data-testid="wizard-step-done">
      <p className={styles.lead}>{t('setup.done.lead')}</p>
      <AccountList
        accounts={props.data.accounts}
        keys={props.data.keys}
        rows={props.rows}
        focusAccountId={props.focusAccountId}
      />
      <AccountActions actions={props.actions} />
      <DepthControl settings={props.settings} showWarning={props.showSettingsWarning} />
      <FinishActions
        actions={props.actions}
        rollback={props.rollback}
        depth={props.settings.depth}
        canFinish={canFinish}
      />
    </div>
  );
}

export function EmptyAccounts({
  active,
  report,
}: {
  active: boolean;
  report: SetupPageComponentProps['report'];
}) {
  const { t } = useI18n();
  return (
    <div className={styles.state} data-testid="wizard-empty">
      <Icon name="user" size={28} />
      <p>{t('setup.empty.text')}</p>
      <button
        className={styles.primary}
        onClick={() => report({ type: 'rollback', target: active ? 'connect' : 'key' })}
        data-testid="wizard-empty-cta"
      >
        {t('setup.empty.cta')}
      </button>
    </div>
  );
}

function AccountList({
  accounts,
  keys,
  rows,
  focusAccountId,
}: {
  accounts: FlickrAccount[];
  keys: ApiKey[];
  rows: RefObject<Map<number, HTMLLIElement>>;
  focusAccountId: number | undefined;
}) {
  const { t } = useI18n();
  return (
    <ul
      className={styles.accounts}
      aria-label={t('setup.done.accounts')}
      data-testid="wizard-accounts"
    >
      {accounts.map((account) => (
        <li
          key={account.id}
          ref={(node) => {
            if (node) rows.current.set(account.id, node);
            else rows.current.delete(account.id);
          }}
          tabIndex={-1}
          data-testid={`wizard-account-${account.nsid}`}
          data-setup-focus={account.id === focusAccountId ? 'true' : undefined}
        >
          <b>{account.username}</b> <code>{account.nsid}</code>
          <small>
            {t('setup.done.account.meta', {
              label: keys.find((key) => key.id === account.key_id)?.label ?? '',
            })}
          </small>
          <AccountStatus account={account} />
        </li>
      ))}
    </ul>
  );
}

function AccountStatus({ account }: { account: FlickrAccount }) {
  const { t } = useI18n();
  return (
    <span
      className={account.status === 'ok' ? styles.okText : styles.badText}
      data-testid={`wizard-account-status-${account.nsid}`}
    >
      <Icon name={account.status === 'ok' ? 'check' : 'alert'} size={14} />{' '}
      {t(accountStatusCopy[account.status])}
    </span>
  );
}

function AccountActions({ actions }: { actions: DoneActions }) {
  const { t } = useI18n();
  return (
    <div className={styles.actions}>
      <button
        type="button"
        className={styles.secondary}
        onClick={actions.goToConnect}
        data-testid="wizard-add-account"
      >
        {t('setup.done.add')}
      </button>
      <button
        type="button"
        className={styles.ghost}
        onClick={() => actions.setAddKeyOpen(true)}
        data-testid="wizard-add-key"
      >
        {t('setup.done.add-key')}
      </button>
    </div>
  );
}

function DepthControl({ settings, showWarning }: { settings: DoneSettings; showWarning: boolean }) {
  const { t } = useI18n();
  return (
    <div className={styles.depth}>
      <label htmlFor="wizard-depth" data-testid="wizard-depth-value">
        {t('setup.done.depth.label', { depth: settings.depth })}
      </label>
      <input
        id="wizard-depth"
        type="range"
        min={1}
        max={settings.depthMax}
        step={1}
        value={settings.depth}
        onChange={(event) => settings.setDepthChoice(Number(event.target.value))}
        aria-valuetext={String(settings.depth)}
        aria-describedby="wizard-depth-help"
        data-testid="wizard-depth"
      />
      <small id="wizard-depth-help">{t('setup.done.depth.help', { max: settings.depthMax })}</small>
      {settings.query.isError && showWarning ? <SettingsWarning settings={settings} /> : null}
    </div>
  );
}

function SettingsWarning({ settings }: { settings: DoneSettings }) {
  const { t } = useI18n();
  return (
    <div role="status" className={styles.warn} data-testid="wizard-stale-banner">
      <span>{t('setup.stale.settings')}</span>
      <button
        type="button"
        className={styles.ghost}
        onClick={() => void settings.query.refetch()}
        data-testid="wizard-stale-retry"
      >
        {t('common.retry')}
      </button>
    </div>
  );
}

function FinishActions({
  actions,
  rollback,
  depth,
  canFinish,
}: {
  actions: DoneActions;
  rollback?: ReactNode;
  depth: number;
  canFinish: boolean;
}) {
  const { t } = useI18n();
  return (
    <div className={styles.actions}>
      {actions.finishError ? <FinishError error={actions.finishError} /> : null}
      {canFinish ? (
        <button
          type="button"
          className={styles.primary}
          disabled={actions.starting}
          onClick={() => void actions.finish(depth)}
          data-testid="wizard-finish"
        >
          {actions.starting ? t('setup.done.finishing') : t('setup.done.finish')}
        </button>
      ) : null}
      {rollback}
    </div>
  );
}

function FinishError({ error }: { error: 'finish' | 'depth' }) {
  const { t } = useI18n();
  const key = error === 'depth' ? 'setup.done.error.depth' : 'setup.done.error.finish';
  return (
    <div role="alert" className={styles.error} data-testid="wizard-done-error">
      {t(key)}
    </div>
  );
}
