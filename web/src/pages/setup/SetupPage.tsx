import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useLocation } from 'react-router';
import {
  connectFlickrAccount,
  flickrAccountCallback,
} from '../../shared/api/gen/flickr-accounts/flickr-accounts';
import { getMeCrawlQueue } from '../../shared/api/gen/crawl/crawl';
import type { ApiKey } from '../../shared/api/gen/models';
import { Icon, useI18n } from '../../shared';
import type { MessageKey } from '../../shared/i18n';
import { checkAndSaveKey, loadKeys } from '../../features/keys/api';
import styles from './SetupPage.module.css';

type WizardError =
  | 'load'
  | 'key-format'
  | 'secret-format'
  | 'key-invalid'
  | 'key-duplicate'
  | 'key-network'
  | 'key-node'
  | 'connect'
  | 'denied'
  | 'expired'
  | 'callback'
  | 'progress';
const keyPattern = /^[a-f\d]{32}$/i;
const secretPattern = /^[a-f\d]{16}$/i;
const mosaic = [
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

export function SetupPage() {
  const { t } = useI18n();
  const location = useLocation();
  const queryClient = useQueryClient();
  const heading = useRef<HTMLHeadingElement>(null);
  const keyField = useRef<HTMLInputElement>(null);
  const secretField = useRef<HTMLInputElement>(null);
  // The callback can return to this route when the deployment config points
  // Flickr's callback at the web app. The existing callback API remains the
  // authority for state, verifier and account validation.
  const callback = useMemo(() => {
    const params = new URLSearchParams(location.search);
    const state = params.get('state');
    const oauthToken = params.get('oauth_token');
    const verifier = params.get('oauth_verifier');
    return state && oauthToken
      ? { state, oauth_token: oauthToken, ...(verifier ? { oauth_verifier: verifier } : {}) }
      : null;
  }, [location.search]);
  const [stepState, setStepState] = useState<1 | 2 | 3 | null>(() => {
    const params = new URLSearchParams(location.search);
    if (params.get('step') === '3') return 3;
    return params.has('error') ? 2 : null;
  });
  const [selectedKeyId, setSelectedKeyId] = useState<number | null>(null);
  const [key, setKey] = useState('');
  const [secret, setSecret] = useState('');
  const [showSecret, setShowSecret] = useState(false);
  const [busy, setBusy] = useState(callback !== null);
  const [error, setError] = useState<WizardError | null>(() => {
    const param = new URLSearchParams(location.search).get('error');
    return param === 'denied' || param === 'expired' ? param : null;
  });
  const [progress, setProgress] = useState<{ backlog: number; eta: string } | null>(null);
  const [callbackDone, setCallbackDone] = useState(false);

  const keysQuery = useQuery({
    queryKey: ['/keys'],
    queryFn: ({ signal }) => loadKeys(signal),
    retry: false,
  });
  const keys: ApiKey[] = keysQuery.data ?? [];
  const validKeys = keys.filter((item) => item.status === 'valid');
  const activeKey = keys.find((item) => item.id === selectedKeyId) ?? validKeys.at(-1) ?? null;
  const step = stepState ?? (activeKey && !callback ? 2 : 1);
  const setStep = (next: 1 | 2 | 3) => setStepState(next);

  useEffect(() => {
    if (!callback || callbackDone) return;
    void flickrAccountCallback(callback)
      .then(async (response) => {
        if (response.status === 200) {
          setCallbackDone(true);
          setStep(3);
          setProgress({ backlog: 0, eta: t('user.wizard.progress.starting') });
          await queryClient.invalidateQueries({ queryKey: ['/me/crawl/queue'] });
          void getMeCrawlQueue()
            .then((queue) => {
              if (queue.status === 200) {
                const backlog = queue.data.lanes.reduce((sum, lane) => sum + lane.backlog_calls, 0);
                setProgress({
                  backlog,
                  eta:
                    backlog > 0
                      ? t('user.wizard.progress.queued')
                      : t('user.wizard.progress.starting'),
                });
              }
            })
            .catch(() => undefined);
          window.history.replaceState({}, '', '/onboarding?step=3');
        } else {
          setStep(2);
          setError(response.status === 400 ? 'denied' : 'callback');
        }
      })
      .catch(() => {
        setStep(2);
        setError('callback');
      })
      .finally(() => setBusy(false));
  }, [callback, callbackDone, queryClient, t]);

  useEffect(() => {
    if (step !== 3 || progress !== null) return;
    void getMeCrawlQueue()
      .then((queue) => {
        if (queue.status === 200) {
          const backlog = queue.data.lanes.reduce((sum, lane) => sum + lane.backlog_calls, 0);
          setProgress({
            backlog,
            eta: t(backlog > 0 ? 'user.wizard.progress.queued' : 'user.wizard.progress.starting'),
          });
        } else setError('progress');
      })
      .catch(() => setError('progress'));
  }, [progress, step, t]);

  useEffect(() => {
    document.title = `${t('user.wizard.title')} · XFlickr`;
    heading.current?.focus();
  }, [step, t]);

  const submitKey = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    const cleanKey = key.replace(/\s/g, '');
    const cleanSecret = secret.replace(/\s/g, '');
    if (!keyPattern.test(cleanKey)) {
      setError('key-format');
      keyField.current?.focus();
      return;
    }
    if (!secretPattern.test(cleanSecret)) {
      setError('secret-format');
      secretField.current?.focus();
      return;
    }
    setBusy(true);
    try {
      const response = await checkAndSaveKey({
        label: t('user.wizard.key.default-label'),
        key: cleanKey,
        secret: cleanSecret,
      });
      if (response.status === 201) {
        queryClient.setQueryData<ApiKey[]>(['/keys'], [...keys, response.data]);
        setSelectedKeyId(response.data.id);
        setKey('');
        setSecret('');
        setStep(2);
        await queryClient.invalidateQueries({ queryKey: ['/keys'] });
      } else if (response.status === 409) setError('key-duplicate');
      else if (response.status === 503) setError('key-node');
      else if (response.status === 422 && response.data.code === 'key_invalid')
        setError('key-invalid');
      else setError('key-network');
    } catch {
      setError('key-network');
    } finally {
      setBusy(false);
    }
  };

  const authorize = async () => {
    if (!activeKey || busy) return;
    setBusy(true);
    setError(null);
    try {
      const response = await connectFlickrAccount({ key_id: activeKey.id });
      if (response.status === 200) window.location.assign(response.data.authorize_url);
      else setError('connect');
    } catch {
      setError('connect');
    } finally {
      setBusy(false);
    }
  };

  const setWizardStep = (next: 1 | 2 | 3) => {
    setError(null);
    setStep(next);
  };
  const retryLoad = () => {
    setError(null);
    void keysQuery.refetch();
  };
  const stepName =
    step === 1
      ? t('user.wizard.step.key')
      : step === 2
        ? t('user.wizard.step.connect')
        : t('user.wizard.step.done');
  const errorCopy: Partial<Record<WizardError, MessageKey>> = {
    load: 'user.wizard.error.text',
    'key-format': 'user.wizard.key.error.key-format',
    'secret-format': 'user.wizard.key.error.secret-format',
    'key-invalid': 'user.wizard.key.error.invalid',
    'key-duplicate': 'user.wizard.key.error.duplicate',
    'key-network': 'user.wizard.key.error.network',
    'key-node': 'user.wizard.key.error.no-node',
    connect: 'user.wizard.connect.error.start',
    denied: 'user.wizard.connect.denied',
    expired: 'user.wizard.connect.expired',
    callback: 'user.wizard.connect.error.start',
    progress: 'user.wizard.progress.error',
  };

  return (
    <section className={styles.page} data-testid="wizard-page">
      <div className={styles.card}>
        <div className={styles.art} aria-hidden="true" data-testid="wizard-art">
          {Array.from({ length: 18 }, (_, i) => (
            <i
              key={i}
              style={{ background: `linear-gradient(135deg,${mosaic[i % mosaic.length]})` }}
            />
          ))}
        </div>
        <div
          className={styles.body}
          data-testid="wizard-body"
          aria-busy={keysQuery.isLoading || busy}
        >
          {keysQuery.isLoading ? (
            <div className={styles.loading} role="status" data-testid="wizard-skeleton">
              <span>{t('user.wizard.loading')}</span>
              <i />
              <i />
              <i />
            </div>
          ) : keysQuery.isError && (keysQuery.error as { status?: number }).status === 403 ? (
            <div className={styles.state} data-testid="wizard-forbidden" tabIndex={-1}>
              <Icon name="lock" />
              <h1>{t('user.wizard.forbidden.title')}</h1>
              <p>{t('user.wizard.forbidden.text')}</p>
              <Link className={styles.primary} to="/" data-testid="wizard-forbidden-cta">
                {t('user.wizard.forbidden.cta')}
              </Link>
            </div>
          ) : keysQuery.isError ? (
            <div className={styles.state} data-testid="wizard-error" tabIndex={-1}>
              <Icon name="alert" />
              <h1>{t('user.wizard.error.title')}</h1>
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
                aria-valuetext={t('user.wizard.step', { n: step, total: 3, name: stepName })}
                aria-label={t('user.wizard.steps.aria')}
                data-testid="wizard-steps"
              >
                {[1, 2, 3].map((n) => (
                  <i key={n} className={n <= step ? styles.filled : ''} />
                ))}
              </div>
              <p className={styles.eyebrow}>
                {t('user.wizard.step', { n: step, total: 3, name: stepName })}
              </p>
              <h1
                ref={heading}
                tabIndex={-1}
                data-testid={`wizard-step-${step === 1 ? 'key' : step === 2 ? 'connect' : 'done'}`}
              >
                {t(
                  step === 1
                    ? 'user.wizard.key.title'
                    : step === 2
                      ? 'user.wizard.connect.title'
                      : 'user.wizard.done.title',
                )}
              </h1>
              {step === 1 ? (
                <>
                  <p className={styles.lead}>{t('user.wizard.key.lead')}</p>
                  <aside className={styles.callout} data-testid="wizard-key-why">
                    <b>{t('user.wizard.key.why.title')}</b>
                    <p>{t('user.wizard.key.why.text')}</p>
                  </aside>
                  {error && error !== 'key-format' && error !== 'secret-format' ? (
                    <div role="alert" className={styles.error} data-testid="wizard-key-error">
                      {t(errorCopy[error] ?? 'user.wizard.key.error.network')}
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
                        onChange={(event) => setKey(event.target.value)}
                        aria-describedby="wizard-key-help"
                        aria-invalid={
                          error === 'key-format' ||
                          error === 'key-invalid' ||
                          error === 'key-duplicate'
                        }
                        autoComplete="off"
                        data-testid="wizard-key-input"
                      />
                    </label>
                    <small
                      id="wizard-key-help"
                      data-testid={error === 'key-format' ? 'wizard-key-error-key' : undefined}
                    >
                      {t(
                        error === 'key-format'
                          ? 'user.wizard.key.error.key-format'
                          : error === 'key-invalid' || error === 'key-duplicate'
                            ? (errorCopy[error] ?? 'user.wizard.key.help.key')
                            : 'user.wizard.key.help.key',
                      )}
                    </small>
                    <label>
                      {t('user.wizard.key.field.secret')}
                      <span className={styles.secret}>
                        <input
                          ref={secretField}
                          type={showSecret ? 'text' : 'password'}
                          className={styles.mono}
                          value={secret}
                          onChange={(event) => setSecret(event.target.value)}
                          aria-describedby="wizard-secret-help"
                          aria-invalid={error === 'secret-format'}
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
                    <small
                      id="wizard-secret-help"
                      data-testid={
                        error === 'secret-format' ? 'wizard-key-error-secret' : undefined
                      }
                    >
                      {t(
                        error === 'secret-format'
                          ? 'user.wizard.key.error.secret-format'
                          : 'user.wizard.key.help.secret',
                      )}
                    </small>
                    <a
                      href="https://www.flickr.com/services/apps/create"
                      target="_blank"
                      rel="noreferrer"
                      data-testid="wizard-key-create-link"
                    >
                      {t('user.wizard.key.create-link')}
                    </a>
                    <button
                      className={styles.primary}
                      disabled={busy}
                      type="submit"
                      data-testid="wizard-key-submit"
                    >
                      {busy ? t('user.wizard.key.checking') : t('user.wizard.key.submit')}
                    </button>
                  </form>
                </>
              ) : null}
              {step === 2 ? (
                <>
                  <p className={styles.lead}>{t('user.wizard.connect.lead')}</p>
                  {error ? (
                    <div role="alert" className={styles.error} data-testid="wizard-connect-error">
                      {t(errorCopy[error] ?? 'user.wizard.connect.error.start')}
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
                    <small>{t('user.wizard.connect.leave-warning')}</small>
                  </div>
                  {validKeys.length > 1 ? (
                    <label>
                      {t('user.wizard.connect.key-select')}
                      <select
                        value={activeKey?.id ?? ''}
                        onChange={(event) => setSelectedKeyId(Number(event.target.value))}
                        data-testid="wizard-key-select"
                      >
                        {keys
                          .filter((item) => item.status === 'valid')
                          .map((item) => (
                            <option key={item.id} value={item.id}>
                              {item.label}
                            </option>
                          ))}
                      </select>
                    </label>
                  ) : null}
                  <div className={styles.actions}>
                    <button
                      className={styles.primary}
                      disabled={busy || !activeKey}
                      onClick={() => void authorize()}
                      data-testid="wizard-connect-allow"
                    >
                      {busy ? t('user.wizard.connect.waiting') : t('user.wizard.connect.authorize')}
                    </button>
                    <button
                      className={styles.ghost}
                      onClick={() => setWizardStep(1)}
                      data-testid="wizard-connect-back"
                    >
                      {t('user.wizard.connect.back')}
                    </button>
                  </div>
                </>
              ) : null}
              {step === 3 ? (
                <>
                  {error ? (
                    <div role="alert" className={styles.error} data-testid="wizard-done-error">
                      {t(errorCopy[error] ?? 'user.wizard.progress.error')}{' '}
                      <button
                        onClick={() => {
                          setError(null);
                          void getMeCrawlQueue()
                            .then((queue) => {
                              if (queue.status === 200)
                                setProgress({
                                  backlog: queue.data.lanes.reduce(
                                    (sum, lane) => sum + lane.backlog_calls,
                                    0,
                                  ),
                                  eta: t('user.wizard.progress.queued'),
                                });
                            })
                            .catch(() => setError('progress'));
                        }}
                      >
                        {t('common.retry')}
                      </button>
                    </div>
                  ) : null}
                  <div className={styles.progressCard} data-testid="wizard-crawl-progress">
                    <span className={styles.liveDot} /> <b>{t('user.wizard.progress.title')}</b>
                    <p>
                      {progress
                        ? t(
                            progress.backlog > 0
                              ? 'user.wizard.progress.backlog'
                              : 'user.wizard.progress.starting',
                            { count: progress.backlog },
                          )
                        : t('user.wizard.progress.loading')}
                    </p>
                    {progress ? (
                      <div className={styles.track}>
                        <i style={{ width: `${progress.backlog > 0 ? 18 : 5}%` }} />
                      </div>
                    ) : (
                      <div className={styles.track}>
                        <i />
                      </div>
                    )}
                  </div>
                  <p className={styles.note}>{t('user.wizard.progress.note')}</p>
                  <Link className={styles.primary} to="/" data-testid="wizard-home">
                    {t('user.wizard.done.home')}
                  </Link>
                  <button
                    className={styles.ghost}
                    onClick={() => setWizardStep(2)}
                    data-testid="wizard-add-account"
                  >
                    {t('user.wizard.connect.retry')}
                  </button>
                </>
              ) : null}
              <div
                className={styles.announce}
                role="status"
                aria-live="polite"
                data-testid="wizard-live-region"
              >
                {step === 2 && activeKey ? t('user.wizard.key.ok') : ''}
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
