import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import {
  connectFlickrAccount,
  flickrAccountCallback,
} from '../../shared/api/gen/flickr-accounts/flickr-accounts';
import { getMeCrawlQueue } from '../../shared/api/gen/crawl/crawl';
import type { ApiKey, FlickrAccount } from '../../shared/api/gen/models';
import { Icon, useI18n, useLoadingTiming } from '../../shared';
import type { MessageKey } from '../../shared/i18n';
import { checkAndSaveKey, loadAccounts, loadKeys } from '../../features/keys/api';
import styles from './SetupPage.module.css';

type Step = 1 | 2 | 3;
type KeyBanner = 'invalid' | 'network' | 'node';
type FieldError = 'format' | 'duplicate';
type ConnectError = 'denied' | 'expired' | 'start';

const keyPattern = /^[a-f\d]{32}$/i;
const secretPattern = /^[a-f\d]{16}$/i;
const SKELETON_DELAY = 150;
/** Polling on step 3 while the live stream is not available: 10, 20, 40, 60 s. */
const POLL_MS = [10_000, 20_000, 40_000, 60_000];
const PALETTE = [
  '#e9a97c,#8fd0a4',
  '#7ca8e9,#7cc9d0',
  '#8fd0a4,#7ca8e9',
  '#d98fb4,#9a8fe0',
  '#f0d27a,#e9a97c',
  '#9a8fe0,#f0d27a',
  '#7cc9d0,#b4c97c',
  '#e07c7c,#d98fb4',
  '#b4c97c,#e07c7c',
];

/** Background of mosaic tile `index`; a neutral tile when the palette is empty. */
export function tileBackground(palette: readonly string[], index: number): string {
  if (palette.length === 0) return 'var(--sunken)';
  return `linear-gradient(135deg,${palette[index % palette.length]})`;
}

const clean = (value: string) => value.replace(/\s/g, '');
const statusOf = (error: unknown) => (error as { status?: number } | null)?.status;

const accountStatusCopy: Record<FlickrAccount['status'], MessageKey> = {
  ok: 'user.wizard.done.account.ok',
  reconnect_required: 'user.wizard.done.account.reconnect',
  revoked: 'user.wizard.done.account.revoked',
};

const connectErrorCopy: Record<ConnectError, MessageKey> = {
  denied: 'user.wizard.connect.denied',
  expired: 'user.wizard.connect.expired',
  start: 'user.wizard.connect.error.start',
};

const bannerCopy: Record<KeyBanner, MessageKey> = {
  invalid: 'user.wizard.key.error.invalid',
  network: 'user.wizard.key.error.network',
  node: 'user.wizard.key.error.no-node',
};

export function SetupPage() {
  const { t } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const heading = useRef<HTMLHeadingElement>(null);
  const keyField = useRef<HTMLInputElement>(null);
  const secretField = useRef<HTMLInputElement>(null);
  const keyBannerRef = useRef<HTMLDivElement>(null);
  const rows = useRef(new Map<number, HTMLLIElement>());
  const callbackStarted = useRef<string | null>(null);
  const announced = useRef<number | null>(null);
  const abort = useRef<AbortController | null>(null);

  const params = useMemo(() => new URLSearchParams(location.search), [location.search]);
  const hintStep = params.get('step');
  const connectedId = Number(params.get('connected')) || null;
  // The server redirects the browser here after Flickr; the existing callback API
  // stays the authority when the deployment points Flickr's callback at the web app.
  const callback = useMemo(() => {
    const state = params.get('state');
    const oauthToken = params.get('oauth_token');
    const verifier = params.get('oauth_verifier');
    return state && oauthToken
      ? { state, oauth_token: oauthToken, ...(verifier ? { oauth_verifier: verifier } : {}) }
      : null;
  }, [params]);

  const [override, setOverride] = useState<Step | null>(null);
  const [selectedKeyId, setSelectedKeyId] = useState<number | null>(null);
  const [callbackAccount, setCallbackAccount] = useState<FlickrAccount | null>(null);
  const [callbackPending, setCallbackPending] = useState(callback !== null);
  const [liveText, setLiveText] = useState('');

  const [key, setKey] = useState('');
  const [secret, setSecret] = useState('');
  const [showSecret, setShowSecret] = useState(false);
  const [touched, setTouched] = useState({ key: false, secret: false });
  const [submitted, setSubmitted] = useState(false);
  const [serverErrors, setServerErrors] = useState<{ key?: FieldError; secret?: FieldError }>({});
  const [banner, setBanner] = useState<KeyBanner | null>(null);
  const [sending, setSending] = useState(false);

  const [waiting, setWaiting] = useState(false);
  const [connectError, setConnectError] = useState<ConnectError | null>(() => {
    const param = params.get('error');
    return param === 'denied' || param === 'expired' ? param : null;
  });

  const keysQuery = useQuery({
    queryKey: ['/keys'],
    queryFn: ({ signal }) => loadKeys(signal),
    retry: false,
  });
  const accountsQuery = useQuery({
    queryKey: ['/flickr-accounts'],
    queryFn: ({ signal }) => loadAccounts(signal),
    retry: false,
  });

  const keys: ApiKey[] = useMemo(() => keysQuery.data ?? [], [keysQuery.data]);
  const validKeys = keys.filter((item) => item.status === 'valid');
  const activeKey = keys.find((item) => item.id === selectedKeyId) ?? validKeys.at(-1) ?? null;
  const accountList = useMemo(() => {
    const base = accountsQuery.data ?? [];
    return callbackAccount && !base.some((item) => item.id === callbackAccount.id)
      ? [...base, callbackAccount]
      : base;
  }, [accountsQuery.data, callbackAccount]);
  const hasAccount = accountList.length > 0;

  // The server decides the step; `?step=` is only a hint that is honoured when the
  // data supports it.
  const derived: Step = hasAccount ? 3 : activeKey ? 2 : 1;
  const hinted: Step | null =
    hintStep === '3' && (activeKey || hasAccount) ? 3 : hintStep === '2' && activeKey ? 2 : null;
  const step: Step = override ?? hinted ?? derived;

  const initialLoading = keysQuery.isPending || accountsQuery.isPending || callbackPending;
  const showSkeleton = useLoadingTiming(initialLoading, SKELETON_DELAY);
  const forbidden = statusOf(keysQuery.error) === 403 || statusOf(accountsQuery.error) === 403;
  const loadFailed =
    !forbidden && (keysQuery.isError || (accountsQuery.isError && !accountsQuery.data));
  const view = initialLoading
    ? 'loading'
    : forbidden
      ? 'forbidden'
      : loadFailed
        ? 'error'
        : 'steps';

  const queueQuery = useQuery({
    queryKey: ['/me/crawl/queue'],
    queryFn: async ({ signal }) => {
      const response = await getMeCrawlQueue({ signal });
      if (response.status !== 200)
        throw Object.assign(new Error('Could not load progress'), { status: response.status });
      return response.data;
    },
    enabled: view === 'steps' && step === 3 && hasAccount,
    retry: false,
  });
  const backlog = queueQuery.data?.lanes.reduce((sum, lane) => sum + lane.backlog_calls, 0) ?? 0;

  useEffect(() => {
    if (!callback || callbackStarted.current === callback.state) return;
    callbackStarted.current = callback.state;
    void flickrAccountCallback(callback)
      .then((response) => {
        if (response.status === 200) {
          setCallbackAccount(response.data);
          setOverride(3);
          void queryClient.invalidateQueries({ queryKey: ['/flickr-accounts'] });
          void navigate(`/onboarding?step=3&connected=${response.data.id}`, { replace: true });
        } else {
          setOverride(2);
          setConnectError(
            response.status === 400 ? (callback.oauth_verifier ? 'expired' : 'denied') : 'start',
          );
        }
      })
      .catch(() => {
        setOverride(2);
        setConnectError('start');
      })
      .finally(() => setCallbackPending(false));
  }, [callback, navigate, queryClient]);

  useEffect(() => {
    document.title = `${t('user.wizard.title')} · XFlickr`;
  }, [t]);

  useEffect(() => {
    if (view !== 'loading') heading.current?.focus();
  }, [step, view]);

  useEffect(() => {
    if (banner) keyBannerRef.current?.focus();
  }, [banner]);

  // The account that was just connected is announced and focused once.
  const connectedAccount =
    view === 'steps' && step === 3
      ? accountList.find((item) => item.id === (connectedId ?? callbackAccount?.id))
      : undefined;
  useEffect(() => {
    if (!connectedAccount || announced.current === connectedAccount.id) return;
    announced.current = connectedAccount.id;
    rows.current.get(connectedAccount.id)?.focus();
  }, [connectedAccount]);

  // Step 3 is live; without a stream it polls with backoff and pauses while hidden.
  const refetchAccounts = accountsQuery.refetch;
  const refetchQueue = queueQuery.refetch;
  useEffect(() => {
    if (view !== 'steps' || step !== 3) return undefined;
    let failures = 0;
    let stopped = false;
    let timer = 0;
    const schedule = () => {
      timer = window.setTimeout(() => void tick(), POLL_MS[Math.min(failures, POLL_MS.length - 1)]);
    };
    const tick = async () => {
      if (!document.hidden) {
        const [accounts, queue] = await Promise.all([refetchAccounts(), refetchQueue()]);
        failures = accounts.isError || queue.isError ? failures + 1 : 0;
      }
      if (!stopped) schedule();
    };
    schedule();
    return () => {
      stopped = true;
      window.clearTimeout(timer);
    };
  }, [view, step, refetchAccounts, refetchQueue]);

  const keyFormatInvalid = !keyPattern.test(clean(key));
  const secretFormatInvalid = !secretPattern.test(clean(secret));
  const keyError: FieldError | null =
    serverErrors.key ?? ((touched.key || submitted) && keyFormatInvalid ? 'format' : null);
  const secretError: FieldError | null =
    serverErrors.secret ?? ((touched.secret || submitted) && secretFormatInvalid ? 'format' : null);

  const goTo = (next: Step) => {
    setConnectError(null);
    setBanner(null);
    setOverride(next);
  };

  const submitKey = async (event?: FormEvent) => {
    event?.preventDefault();
    if (sending) return;
    setSubmitted(true);
    setBanner(null);
    setServerErrors({});
    if (keyFormatInvalid) {
      keyField.current?.focus();
      return;
    }
    if (secretFormatInvalid) {
      secretField.current?.focus();
      return;
    }
    setSending(true);
    try {
      const response = await checkAndSaveKey({
        label: t('user.wizard.key.default-label'),
        key: clean(key),
        secret: clean(secret),
      });
      if (response.status === 201) {
        queryClient.setQueryData<ApiKey[]>(['/keys'], [...keys, response.data]);
        setSelectedKeyId(response.data.id);
        setKey('');
        setSecret('');
        setShowSecret(false);
        setTouched({ key: false, secret: false });
        setSubmitted(false);
        setLiveText(t('user.wizard.key.ok'));
        goTo(2);
        void queryClient.invalidateQueries({ queryKey: ['/keys'] });
      } else if (response.status === 400) {
        const fields = (response.data.details as { fields?: Record<string, unknown> } | undefined)
          ?.fields;
        const next = {
          ...(fields && 'key' in fields ? { key: 'format' as const } : {}),
          ...(fields && 'secret' in fields ? { secret: 'format' as const } : {}),
        };
        if (next.key || next.secret) {
          setServerErrors(next);
          (next.key ? keyField : secretField).current?.focus();
        } else setBanner('network');
      } else if (response.status === 409) {
        setServerErrors({ key: 'duplicate' });
        keyField.current?.focus();
      } else if (response.status === 422 && response.data.code === 'key_invalid')
        setBanner('invalid');
      else if (response.status === 503) setBanner('node');
      else setBanner('network');
    } catch {
      setBanner('network');
    } finally {
      setSending(false);
    }
  };

  const authorize = async () => {
    if (!activeKey || waiting) return;
    const controller = new AbortController();
    abort.current = controller;
    setWaiting(true);
    setConnectError(null);
    try {
      const response = await connectFlickrAccount(
        { key_id: activeKey.id },
        { signal: controller.signal },
      );
      if (response.status === 200) window.location.assign(response.data.authorize_url);
      else {
        setConnectError('start');
        setWaiting(false);
      }
    } catch {
      if (controller.signal.aborted) return;
      setConnectError('start');
      setWaiting(false);
    }
  };

  const cancelConnect = () => {
    abort.current?.abort();
    setWaiting(false);
  };

  const retryLoad = () => {
    if (keysQuery.isError) void keysQuery.refetch();
    if (accountsQuery.isError) void accountsQuery.refetch();
  };

  const stepName =
    step === 1
      ? t('user.wizard.step.key')
      : step === 2
        ? t('user.wizard.step.connect')
        : t('user.wizard.step.done');
  const stepText = t('user.wizard.step', { n: step, total: 3, name: stepName });
  const staleAccounts = accountsQuery.isError && accountsQuery.data !== undefined;
  const canFinish = accountList.some((item) => item.status === 'ok');

  return (
    <section className={styles.page} data-testid="wizard-page">
      <div className={styles.card}>
        <div className={styles.art} aria-hidden="true" data-testid="wizard-art">
          {Array.from({ length: 18 }, (_, i) => (
            <i key={i} style={{ background: tileBackground(PALETTE, i) }} />
          ))}
        </div>
        <div className={styles.body} data-testid="wizard-body" aria-busy={initialLoading}>
          {view === 'loading' ? (
            showSkeleton ? (
              <div className={styles.loading} role="status" data-testid="wizard-skeleton">
                <span>{t('user.wizard.loading')}</span>
                <i className={styles.skBar} />
                <i className={styles.skHead} />
                <i className={styles.skLine} />
                <i className={styles.skLine} />
                <i className={styles.skField} />
              </div>
            ) : null
          ) : view === 'forbidden' ? (
            <div className={styles.state} data-testid="wizard-forbidden">
              <Icon name="lock" size={28} />
              <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
                {t('user.wizard.forbidden.title')}
              </h1>
              <p>{t('user.wizard.forbidden.text')}</p>
              <Link className={styles.primary} to="/" data-testid="wizard-forbidden-cta">
                {t('user.wizard.forbidden.cta')}
              </Link>
            </div>
          ) : view === 'error' ? (
            <div className={styles.state} data-testid="wizard-error">
              <Icon name="alert" size={28} />
              <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
                {t('user.wizard.error.title')}
              </h1>
              <p>{t('user.wizard.error.text')}</p>
              <button
                className={styles.primary}
                onClick={retryLoad}
                data-testid="wizard-error-retry"
              >
                {t('common.retry')}
              </button>
            </div>
          ) : (
            <>
              <div
                className={styles.progress}
                role="progressbar"
                aria-valuenow={step}
                aria-valuemin={1}
                aria-valuemax={3}
                aria-valuetext={stepText}
                aria-label={t('user.wizard.steps.aria')}
                data-testid="wizard-steps"
              >
                {[1, 2, 3].map((n) => (
                  <i key={n} className={n <= step ? styles.filled : ''} />
                ))}
              </div>
              <p className={styles.eyebrow} data-testid="wizard-eyebrow">
                {stepText}
              </p>
              {staleAccounts ? (
                <div role="status" className={styles.warn} data-testid="wizard-stale-banner">
                  <span>{t('user.wizard.stale.accounts')}</span>
                  <button
                    className={styles.ghost}
                    onClick={() => void accountsQuery.refetch()}
                    data-testid="wizard-stale-retry"
                  >
                    {t('common.retry')}
                  </button>
                </div>
              ) : null}

              {step === 1 ? (
                <div className={styles.step} data-testid="wizard-step-key">
                  <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
                    {t('user.wizard.key.title')}
                  </h1>
                  <p className={styles.lead}>{t('user.wizard.key.lead')}</p>
                  <aside className={styles.callout} data-testid="wizard-key-why">
                    <b>{t('user.wizard.key.why.title')}</b>
                    <p>{t('user.wizard.key.why.text')}</p>
                  </aside>
                  {banner ? (
                    <div
                      ref={keyBannerRef}
                      tabIndex={-1}
                      role="alert"
                      className={styles.error}
                      data-testid="wizard-key-error"
                    >
                      {t(bannerCopy[banner])}
                      {banner !== 'invalid' ? (
                        <button
                          type="button"
                          className={styles.ghost}
                          onClick={() => void submitKey()}
                        >
                          {t('common.retry')}
                        </button>
                      ) : null}
                    </div>
                  ) : null}
                  <form
                    onSubmit={(event) => void submitKey(event)}
                    noValidate
                    data-testid="wizard-key-form"
                  >
                    <label>
                      {t('user.wizard.key.field.key')}
                      <input
                        ref={keyField}
                        className={styles.mono}
                        value={key}
                        onChange={(event) => {
                          setKey(event.target.value);
                          setServerErrors((current) => ({ ...current, key: undefined }));
                        }}
                        onBlur={() => setTouched((current) => ({ ...current, key: true }))}
                        aria-describedby={`wizard-key-help${keyError ? ' wizard-key-err' : ''}`}
                        aria-invalid={keyError !== null || banner === 'invalid'}
                        autoComplete="off"
                        data-testid="wizard-key-input"
                      />
                    </label>
                    <small id="wizard-key-help">{t('user.wizard.key.help.key')}</small>
                    {keyError ? (
                      <small
                        id="wizard-key-err"
                        className={styles.fieldError}
                        data-testid="wizard-key-error-key"
                      >
                        <Icon name="alert" size={14} />{' '}
                        {t(
                          keyError === 'duplicate'
                            ? 'user.wizard.key.error.duplicate'
                            : 'user.wizard.key.error.key-format',
                        )}
                      </small>
                    ) : null}
                    <label>
                      {t('user.wizard.key.field.secret')}
                      <span className={styles.secret}>
                        <input
                          ref={secretField}
                          type={showSecret ? 'text' : 'password'}
                          className={styles.mono}
                          value={secret}
                          onChange={(event) => {
                            setSecret(event.target.value);
                            setServerErrors((current) => ({ ...current, secret: undefined }));
                          }}
                          onBlur={() => setTouched((current) => ({ ...current, secret: true }))}
                          aria-describedby={`wizard-secret-help${secretError ? ' wizard-secret-err' : ''}`}
                          aria-invalid={secretError !== null}
                          autoComplete="new-password"
                          data-testid="wizard-key-secret"
                        />
                        <button
                          type="button"
                          aria-label={t(
                            showSecret ? 'user.wizard.key.hide' : 'user.wizard.key.show',
                          )}
                          aria-pressed={showSecret}
                          onClick={() => setShowSecret(!showSecret)}
                          data-testid="wizard-key-secret-toggle"
                        >
                          <Icon name="eye" />
                        </button>
                      </span>
                    </label>
                    <small id="wizard-secret-help">{t('user.wizard.key.help.secret')}</small>
                    {secretError ? (
                      <small
                        id="wizard-secret-err"
                        className={styles.fieldError}
                        data-testid="wizard-key-error-secret"
                      >
                        <Icon name="alert" size={14} /> {t('user.wizard.key.error.secret-format')}
                      </small>
                    ) : null}
                    <a
                      href="https://www.flickr.com/services/apps/create"
                      target="_blank"
                      rel="noreferrer"
                      data-testid="wizard-key-create-link"
                    >
                      {t('user.wizard.key.create-link')}
                    </a>
                    <div className={styles.actions}>
                      <button
                        className={styles.primary}
                        disabled={sending}
                        type="submit"
                        data-testid="wizard-key-submit"
                      >
                        {sending ? t('user.wizard.key.checking') : t('user.wizard.key.submit')}
                      </button>
                      {hasAccount ? (
                        <button
                          type="button"
                          className={styles.ghost}
                          onClick={() => goTo(3)}
                          data-testid="wizard-key-back"
                        >
                          {t('user.wizard.connect.back')}
                        </button>
                      ) : null}
                    </div>
                  </form>
                </div>
              ) : null}

              {step === 2 ? (
                <div className={styles.step} data-testid="wizard-step-connect">
                  <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
                    {t('user.wizard.connect.title')}
                  </h1>
                  <p className={styles.lead}>{t('user.wizard.connect.lead')}</p>
                  {connectError ? (
                    <div
                      role="alert"
                      className={styles.error}
                      data-testid={
                        connectError === 'denied'
                          ? 'wizard-connect-denied'
                          : connectError === 'expired'
                            ? 'wizard-connect-expired'
                            : 'wizard-connect-error'
                      }
                    >
                      {t(connectErrorCopy[connectError])}{' '}
                      <button
                        className={styles.ghost}
                        onClick={() => void authorize()}
                        data-testid="wizard-connect-retry"
                      >
                        {t('user.wizard.connect.retry')}
                      </button>
                    </div>
                  ) : null}
                  <div className={styles.consent} data-testid="wizard-connect-perms">
                    <b>
                      <Icon name="lock" /> flickr.com
                    </b>
                    <h2>{t('user.wizard.connect.perm.title')}</h2>
                    <ul>
                      <li>{t('user.wizard.connect.perm.read')}</li>
                      <li>{t('user.wizard.connect.perm.private')}</li>
                    </ul>
                    <p>{t('user.wizard.connect.perm.cannot')}</p>
                  </div>
                  {validKeys.length > 1 ? (
                    <label>
                      {t('user.wizard.connect.key-select')}
                      <select
                        value={activeKey?.id ?? ''}
                        onChange={(event) => setSelectedKeyId(Number(event.target.value))}
                        data-testid="wizard-key-select"
                      >
                        {validKeys.map((item) => (
                          <option key={item.id} value={item.id}>
                            {item.label}
                          </option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                  <p id="wizard-leave" className={styles.note}>
                    {t('user.wizard.connect.leave-warning')}
                  </p>
                  {waiting ? (
                    <p role="status" className={styles.note} data-testid="wizard-connect-waiting">
                      {t('user.wizard.connect.waiting')}
                    </p>
                  ) : null}
                  <div className={styles.actions}>
                    <button
                      className={styles.primary}
                      disabled={waiting || !activeKey}
                      aria-describedby="wizard-leave"
                      onClick={() => void authorize()}
                      data-testid="wizard-connect-allow"
                    >
                      {t('user.wizard.connect.authorize')}
                    </button>
                    {waiting ? (
                      <button
                        className={styles.ghost}
                        onClick={cancelConnect}
                        data-testid="wizard-connect-cancel"
                      >
                        {t('user.wizard.connect.cancel')}
                      </button>
                    ) : (
                      <button
                        className={styles.ghost}
                        onClick={() => goTo(1)}
                        data-testid="wizard-connect-back"
                      >
                        {t('user.wizard.connect.back')}
                      </button>
                    )}
                  </div>
                </div>
              ) : null}

              {step === 3 && !hasAccount ? (
                <div className={styles.state} data-testid="wizard-empty">
                  <Icon name="user" size={28} />
                  <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
                    {t('user.wizard.empty.title')}
                  </h1>
                  <p>{t('user.wizard.empty.text')}</p>
                  <button
                    className={styles.primary}
                    onClick={() => goTo(activeKey ? 2 : 1)}
                    data-testid="wizard-empty-cta"
                  >
                    {t('user.wizard.empty.cta')}
                  </button>
                </div>
              ) : null}

              {step === 3 && hasAccount ? (
                <div className={styles.step} data-testid="wizard-step-done">
                  <h1 ref={heading} tabIndex={-1} data-testid="wizard-title">
                    {t('user.wizard.done.title')}
                  </h1>
                  <p className={styles.lead}>{t('user.wizard.progress.note')}</p>
                  <ul
                    className={styles.accounts}
                    aria-label={t('user.wizard.done.accounts')}
                    data-testid="wizard-accounts"
                  >
                    {accountList.map((account) => (
                      <li
                        key={account.id}
                        ref={(node) => {
                          if (node) rows.current.set(account.id, node);
                          else rows.current.delete(account.id);
                        }}
                        tabIndex={-1}
                        data-testid={`wizard-account-${account.nsid}`}
                      >
                        <b>{account.username}</b> <code>{account.nsid}</code>
                        <small>
                          {t('user.wizard.done.account.meta', {
                            label: keys.find((item) => item.id === account.key_id)?.label ?? '',
                          })}
                        </small>
                        <span
                          className={account.status === 'ok' ? styles.okText : styles.badText}
                          data-testid={`wizard-account-status-${account.nsid}`}
                        >
                          <Icon name={account.status === 'ok' ? 'check' : 'alert'} size={14} />{' '}
                          {t(accountStatusCopy[account.status])}
                        </span>
                      </li>
                    ))}
                  </ul>
                  {queueQuery.isError ? (
                    <div role="alert" className={styles.error} data-testid="wizard-done-error">
                      {t('user.wizard.progress.error')}{' '}
                      <button className={styles.ghost} onClick={() => void queueQuery.refetch()}>
                        {t('common.retry')}
                      </button>
                    </div>
                  ) : (
                    <div className={styles.progressCard} data-testid="wizard-crawl-progress">
                      <span className={styles.liveDot} /> <b>{t('user.wizard.progress.title')}</b>
                      <p>
                        {queueQuery.data
                          ? t(
                              backlog > 0
                                ? 'user.wizard.progress.backlog'
                                : 'user.wizard.progress.starting',
                              { count: backlog },
                            )
                          : t('user.wizard.progress.loading')}
                      </p>
                      <div className={styles.track}>
                        <i style={{ width: `${backlog > 0 ? 18 : 5}%` }} />
                      </div>
                    </div>
                  )}
                  <div className={styles.actions}>
                    {canFinish ? (
                      <Link className={styles.primary} to="/" data-testid="wizard-finish">
                        {t('user.wizard.done.home')}
                      </Link>
                    ) : null}
                    <button
                      className={styles.ghost}
                      onClick={() => goTo(2)}
                      data-testid="wizard-add-account"
                    >
                      {t('user.wizard.done.add')}
                    </button>
                    <button
                      className={styles.ghost}
                      onClick={() => goTo(2)}
                      data-testid="wizard-done-back"
                    >
                      {t('user.wizard.connect.back')}
                    </button>
                  </div>
                </div>
              ) : null}
              <div
                className={styles.announce}
                role="status"
                aria-live="polite"
                data-testid="wizard-live-region"
              >
                {connectedAccount
                  ? t('user.wizard.done.announce', { name: connectedAccount.username })
                  : liveText}
              </div>
              <Link className={styles.keysLink} to="/keys">
                {t('shell.nav.keys')}
              </Link>
            </>
          )}
        </div>
      </div>
    </section>
  );
}
