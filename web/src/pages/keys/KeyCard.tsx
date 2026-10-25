import { useState } from 'react';
import { Icon, useI18n } from '../../shared';
import type { MessageKey } from '../../shared/i18n';
import type { ApiKey, FlickrAccount } from '../../shared/api/gen/models';
import type { KeyUsage } from './api';
import { AccountsSection } from './AccountsSection';
import { clock, hintOf, isTerminal, percent, pillOf } from './format';
import type { Hint, Pill } from './format';
import styles from './keys.module.css';

const pillCopy: Record<Pill, MessageKey> = {
  ok: 'keys.state.ok',
  reserve: 'keys.state.reserve',
  exhausted: 'keys.state.exhausted',
  revoked: 'keys.state.revoked',
  invalid: 'keys.state.invalid',
};

const pillTone: Record<Pill, string> = {
  ok: styles.toneOk,
  reserve: styles.toneWarn,
  exhausted: styles.toneBad,
  revoked: styles.toneMuted,
  invalid: styles.toneBad,
};

const barTone: Record<Pill, string> = {
  ok: styles.barOk,
  reserve: styles.barWarn,
  exhausted: styles.barBad,
  revoked: styles.barMuted,
  invalid: styles.barMuted,
};

const hintCopy: Record<Hint, MessageKey> = {
  revoked: 'keys.revoked.hint',
  exhausted: 'keys.exhausted.hint',
  reserve: 'keys.reserve.hint',
  paused: 'keys.paused.hint',
};

type KeyCardProps = {
  apiKey: ApiKey;
  usage: KeyUsage | undefined;
  /** Milliseconds to the window end; `null` when the countdown cannot be trusted. */
  resetMs: number | null;
  accounts: FlickrAccount[] | undefined;
  accountsFailed: boolean;
  busy: boolean;
  connecting: boolean;
  heading: (element: HTMLHeadingElement | null) => void;
  onToggle: (key: ApiKey, paused: boolean) => void;
  onRemove: (key: ApiKey) => void;
  onConnect: (keyId: number) => void;
  onRetryAccounts: () => void;
};

/** One key: state, quota bar, facts, lanes, 24-hour history, accounts and actions. */
export function KeyCard({
  apiKey,
  usage,
  resetMs,
  accounts,
  accountsFailed,
  busy,
  connecting,
  heading,
  onToggle,
  onRemove,
  onConnect,
  onRetryAccounts,
}: KeyCardProps) {
  const { t, locale } = useI18n();
  const number = new Intl.NumberFormat(locale);
  const n = (value: number) => number.format(value);
  const dash = '–';
  const cap = apiKey.per_hour_limit;
  const pill = pillOf(apiKey, usage);
  const hint = hintOf(pill, usage);
  const paused = usage?.paused === true;
  const muted = paused || isTerminal(pill);
  const tone = muted || pill === null ? styles.barMuted : barTone[pill];
  const used = usage?.used;
  const [firstUsed] = useState(used);
  const reserve = usage?.reserve;
  const resetsAt =
    usage === undefined
      ? ''
      : new Intl.DateTimeFormat(locale, { timeStyle: 'short' }).format(
          new Date(usage.window_resets_at),
        );
  const id = String(apiKey.id);
  const lane = (value: number | undefined) => (value === undefined ? dash : n(value));

  return (
    <article
      className={styles.card}
      aria-labelledby={`keys-label-${id}`}
      data-testid={`keys-card-${id}`}
    >
      <header className={styles.cardHead}>
        <div className={styles.cardTitle}>
          <h3
            id={`keys-label-${id}`}
            ref={heading}
            tabIndex={-1}
            title={apiKey.label}
            data-testid="keys-card-label"
          >
            {apiKey.label}
          </h3>
          <code className={styles.faint} data-testid="keys-card-masked">
            {apiKey.key_public}
          </code>
        </div>
        <div className={styles.cardBadges}>
          {pill === null ? null : (
            <span className={`${styles.pill} ${pillTone[pill]}`} data-testid="keys-card-state">
              {t(pillCopy[pill])}
            </span>
          )}
          {paused ? (
            <span className={styles.badge} data-testid="keys-card-paused">
              {t('keys.paused')}
            </span>
          ) : null}
        </div>
      </header>

      <div className={styles.usage}>
        <span data-testid="keys-card-used">
          {t('keys.used')}{' '}
          <span
            key={used}
            className={`${styles.num} ${used !== undefined && used !== firstUsed ? styles.fresh : ''}`}
          >
            {used === undefined ? dash : n(used)}
          </span>{' '}
          / {n(cap)}
        </span>
        <div
          className={`${styles.bar} ${tone}`}
          role="progressbar"
          aria-label={t('keys.used')}
          aria-valuemin={0}
          aria-valuemax={cap}
          aria-valuenow={used ?? 0}
          aria-valuetext={
            used === undefined ? undefined : t('keys.used.aria', { used: n(used), cap: n(cap) })
          }
          data-testid="keys-card-bar"
        >
          <span style={{ width: `${String(percent(used ?? 0, cap))}%` }} />
          {reserve === undefined ? null : (
            <i
              className={styles.tick}
              style={{ left: `${String(percent(cap - reserve, cap))}%` }}
              aria-hidden="true"
            />
          )}
        </div>
      </div>

      <dl className={styles.facts}>
        <div>
          <dt>{t('keys.reserve')}</dt>
          <dd data-testid="keys-card-reserve">
            {reserve === undefined ? dash : t('keys.reserve.value', { count: n(reserve) })}
          </dd>
        </div>
        <div>
          <dt>{t('keys.reset')}</dt>
          <dd className={styles.mono} data-testid="keys-card-reset">
            {usage === undefined ? dash : clock(resetMs)}
          </dd>
        </div>
        <div>
          <dt>{t('keys.burst')}</dt>
          <dd data-testid="keys-card-burst">
            {usage === undefined
              ? dash
              : t('keys.burst.value', { now: n(usage.burst_now), limit: n(apiKey.burst) })}
          </dd>
        </div>
        <div>
          <dt>{t('keys.egress')}</dt>
          <dd className={styles.mono} data-testid="keys-card-node">
            {usage === undefined ? dash : `${usage.node_id} · ${usage.node_ip}`}
          </dd>
        </div>
      </dl>

      <div className={styles.lanes}>
        {(
          [
            ['person', 'keys.lane.person', usage?.person_used],
            ['sizes', 'keys.lane.sizes', usage?.sizes_used],
          ] as const
        ).map(([name, label, value]) => (
          <div key={name} className={styles.lane} data-testid={`keys-card-lane-${name}`}>
            <span>{t(label)}</span>
            <span className={`${styles.bar} ${styles.thin} ${tone}`} aria-hidden="true">
              <span style={{ width: `${String(percent(value ?? 0, cap))}%` }} />
            </span>
            <span className={styles.num}>{lane(value)}</span>
          </div>
        ))}
      </div>

      <div data-testid="keys-card-history">
        <span className={styles.sectionTitle}>
          {t('keys.history')} <small>({t('keys.history.legend')})</small>
        </span>
        <div className={styles.hbars} aria-hidden="true">
          {(usage?.hourly ?? []).map((calls, index) => (
            <span
              key={index}
              className={calls >= cap ? styles.hbarBad : undefined}
              style={{ height: `${String(Math.max(4, percent(calls, cap)))}%` }}
            />
          ))}
        </div>
      </div>

      {hint === null ? null : (
        <p
          className={`${styles.hint} ${hint === 'reserve' ? styles.hintWarn : hint === 'paused' ? '' : styles.hintBad}`}
          data-testid="keys-card-hint"
        >
          <Icon name={hint === 'paused' ? 'pause' : 'alert'} size={14} />{' '}
          {t(hintCopy[hint], { time: resetsAt })}
        </p>
      )}

      <AccountsSection
        keyId={apiKey.id}
        accounts={accounts}
        failed={accountsFailed}
        connecting={connecting}
        onConnect={onConnect}
        onRetry={onRetryAccounts}
      />

      <footer className={styles.actions}>
        {isTerminal(pill) ? null : (
          <button
            type="button"
            className={styles.secondary}
            disabled={busy || usage === undefined}
            aria-label={t(paused ? 'keys.resume.aria' : 'keys.pause.aria', {
              label: apiKey.label,
            })}
            onClick={() => onToggle(apiKey, !paused)}
            data-testid="keys-card-pause"
          >
            {t(paused ? 'keys.resume' : 'keys.pause')}
          </button>
        )}
        <button
          type="button"
          className={styles.dangerGhost}
          disabled={busy}
          aria-label={t('keys.remove.aria', { label: apiKey.label })}
          onClick={() => onRemove(apiKey)}
          data-testid="keys-card-remove"
        >
          {t('keys.remove')}
        </button>
      </footer>
    </article>
  );
}
