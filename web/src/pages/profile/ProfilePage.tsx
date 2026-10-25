import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent, KeyboardEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useBlocker, useNavigate } from 'react-router';
import { useSession } from '../../entities';
import { SESSION_QUERY_KEY } from '../../entities/session/SessionProvider';
import { useI18n } from '../../shared';
import type { Locale } from '../../shared/i18n/catalog';
import { csrfFetchOptions, request, signalSessionExpired } from '../../features/auth/api';
import {
  changeMyPassword,
  listMySessions,
  revokeMySession,
} from '../../shared/api/gen/default/default';
import type { Session } from '../../shared/api/gen/models/session';
import styles from './ProfilePage.module.css';

export function ProfilePage() {
  const { session } = useSession();
  const { t, locale, setLocale } = useI18n();
  const client = useQueryClient();
  const navigate = useNavigate();
  const user = session.status === 'authenticated' ? session.user : undefined;
  const [language, setLanguage] = useState<Locale>(locale);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(true);
  const [sessionsError, setSessionsError] = useState(false);
  const [sessionsHasLoaded, setSessionsHasLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [passwordBusy, setPasswordBusy] = useState(false);
  const displayName = user?.email.split('@')[0] ?? '';
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [message, setMessage] = useState('');
  const [passwordError, setPasswordError] = useState('');
  const [rateLimitedUntil, setRateLimitedUntil] = useState(0);
  const [sessionsUpdatedAt, setSessionsUpdatedAt] = useState(0);
  const [changedElsewhere, setChangedElsewhere] = useState(false);
  const dirty =
    language !== (user?.locale ?? locale) ||
    Boolean(currentPassword || newPassword || confirmPassword);
  const blocker = useBlocker(dirty);
  const [confirmAction, setConfirmAction] = useState<
    { kind: 'one'; session: Session } | { kind: 'others' } | null
  >(null);
  const confirmationTrigger = useRef<HTMLElement | null>(null);
  const sessionsHeading = useRef<HTMLHeadingElement>(null);
  const title = t('profile.title');

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === 'xf.profile.updated' && dirty) setChangedElsewhere(true);
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, [dirty]);

  useEffect(() => {
    document.title = t('shell.document-title', { view: title });
  }, [t, title]);

  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!dirty) return;
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', beforeUnload);
    return () => window.removeEventListener('beforeunload', beforeUnload);
  }, [dirty]);

  useEffect(() => {
    if (typeof BroadcastChannel === 'undefined') return;
    const channel = new BroadcastChannel('xf-profile');
    channel.onmessage = (event: MessageEvent) => {
      if (event.data === 'account-updated' && dirty) setChangedElsewhere(true);
    };
    return () => channel.close();
  }, [dirty]);

  useEffect(() => {
    if (!rateLimitedUntil) return;
    const timer = window.setInterval(() => {
      if (Date.now() >= rateLimitedUntil) {
        setRateLimitedUntil(0);
        window.clearInterval(timer);
      }
    }, 250);
    return () => window.clearInterval(timer);
  }, [rateLimitedUntil]);

  const loadSessions = useCallback(async () => {
    try {
      const result = await listMySessions({ credentials: 'same-origin' });
      signalSessionExpired(result.status);
      if (result.status !== 200) throw new Error('Could not load sessions');
      setSessions(result.data.items ?? []);
      setSessionsUpdatedAt(Date.now());
      setSessionsHasLoaded(true);
      setSessionsError(false);
    } catch {
      setSessionsError(true);
    } finally {
      setSessionsLoading(false);
    }
  }, []);

  useEffect(() => {
    let active = true;
    void listMySessions({ credentials: 'same-origin' })
      .then((result) => {
        if (!active) return;
        signalSessionExpired(result.status);
        if (result.status !== 200) {
          setSessionsError(true);
          return;
        }
        setSessions(result.data.items ?? []);
        setSessionsUpdatedAt(Date.now());
        setSessionsHasLoaded(true);
        setSessionsError(false);
      })
      .catch(() => {
        if (active) setSessionsError(true);
      })
      .finally(() => {
        if (active) setSessionsLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    const onFocus = () => {
      if (sessionsHasLoaded && Date.now() - sessionsUpdatedAt > 60_000) void loadSessions();
    };
    window.addEventListener('focus', onFocus);
    return () => window.removeEventListener('focus', onFocus);
  }, [loadSessions, sessionsHasLoaded, sessionsUpdatedAt]);

  async function saveAccount(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setMessage('');
    try {
      const result = await request<typeof user>('/me', 'PATCH', { locale: language });
      if (result.status !== 200) throw new Error('Could not save profile');
      setLocale(language);
      if (result.data)
        client.setQueryData(SESSION_QUERY_KEY, { status: 'authenticated', user: result.data });
      setMessage(t('profile.toast.saved'));
      setChangedElsewhere(false);
      if (typeof BroadcastChannel !== 'undefined') {
        const channel = new BroadcastChannel('xf-profile');
        channel.postMessage('account-updated');
        channel.close();
      }
      try {
        window.localStorage.setItem('xf.profile.updated', String(Date.now()));
      } catch {
        // Cross-tab notices are optional when storage is unavailable.
      }
    } catch {
      setMessage(t('profile.error.save'));
    } finally {
      setSaving(false);
    }
  }

  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setPasswordError('');
    setMessage('');
    if (newPassword.length < 8) {
      setPasswordError(t('auth.validation.password-short', { min: 8 }));
      return;
    }
    if (newPassword.length > 128) {
      setPasswordError(t('auth.validation.password-long', { max: 128 }));
      return;
    }
    if (newPassword !== confirmPassword) {
      setPasswordError(t('auth.validation.confirm-mismatch'));
      return;
    }
    setPasswordBusy(true);
    try {
      const result = await changeMyPassword(
        {
          current_password: currentPassword,
          new_password: newPassword,
        },
        csrfFetchOptions('POST'),
      );
      if (result.status === 401) {
        signalSessionExpired(result.status);
        return;
      }
      if (
        result.status === 400 &&
        'code' in result.data &&
        result.data.code === 'invalid_current_password'
      ) {
        setPasswordError(t('profile.password.error.current'));
        setCurrentPassword('');
        return;
      }
      if (result.status === 429) {
        const retryAfter = Number(result.headers.get('Retry-After') ?? 60);
        setRateLimitedUntil(Date.now() + retryAfter * 1000);
        setPasswordError(t('profile.error.rate-limited', { minutes: Math.ceil(retryAfter / 60) }));
        return;
      }
      if (result.status !== 204) throw new Error('Could not change password');
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      setMessage(t('profile.toast.password-changed'));
      await loadSessions();
    } catch {
      setPasswordError(t('profile.error.action'));
    } finally {
      setPasswordBusy(false);
    }
  }

  async function confirmRevokeOthers() {
    const result = await request('/auth/sessions', 'DELETE');
    if (result.status === 204) await loadSessions();
    else setSessionsError(true);
  }

  async function confirmRevokeOne(item: Session) {
    const result = await revokeMySession(Number(item.id), csrfFetchOptions('DELETE'));
    signalSessionExpired(result.status);
    if (result.status === 204 || result.status === 404)
      setSessions((current) => current.filter((sessionItem) => sessionItem.id !== item.id));
    else setMessage(t('profile.error.action'));
  }

  async function signOut() {
    await request('/auth/logout', 'POST').catch(() => undefined);
    client.removeQueries();
    client.setQueryData(SESSION_QUERY_KEY, { status: 'unauthenticated' });
    void navigate('/login', { replace: true });
  }

  async function confirmDialog() {
    const action = confirmAction;
    setConfirmAction(null);
    if (action?.kind === 'one') await confirmRevokeOne(action.session);
    if (action?.kind === 'others') await confirmRevokeOthers();
    requestAnimationFrame(() => {
      if (confirmationTrigger.current?.isConnected) confirmationTrigger.current.focus();
      else sessionsHeading.current?.focus();
    });
  }

  function onDialogKeyDown(event: KeyboardEvent<HTMLElement>) {
    if (event.key === 'Escape') {
      event.preventDefault();
      setConfirmAction(null);
      requestAnimationFrame(() => confirmationTrigger.current?.focus());
      return;
    }
    if (event.key !== 'Tab') return;
    const controls = event.currentTarget.querySelectorAll<HTMLElement>(
      'button:not([disabled]), input:not([disabled]), select:not([disabled]), a[href]',
    );
    const first = controls.item(0);
    const last = controls.item(controls.length - 1);
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }

  return (
    <div className={styles.page} data-testid="profile-page">
      <h1 tabIndex={-1} data-testid="profile-title">
        {title}
      </h1>
      {message && (
        <p className={styles.notice} role="status">
          {message}
        </p>
      )}
      <div className={styles.grid}>
        <section
          className={styles.card}
          aria-labelledby="profile-account-title"
          data-testid="profile-account-card"
        >
          <h2 id="profile-account-title">{t('profile.account.title')}</h2>
          <div className={styles.identity}>
            <span className={styles.avatar} data-testid="profile-account-avatar">
              {(displayName[0] ?? user?.email[0] ?? 'U').toUpperCase()}
            </span>
            <div>
              <h3 data-testid="profile-account-name">{displayName || user?.email.split('@')[0]}</h3>
              <p data-testid="profile-account-email">{user?.email}</p>
            </div>
          </div>
          <form onSubmit={(event) => void saveAccount(event)}>
            <label className={styles.field}>
              {t('profile.account.language')}
              <select
                value={language}
                onChange={(event) => setLanguage(event.target.value as Locale)}
                data-testid="profile-account-language"
              >
                <option value="en">{t('common.language.en')}</option>
                <option value="vi">{t('common.language.vi')}</option>
              </select>
            </label>
            <label className={styles.field}>
              {t('profile.account.timezone')}
              <input
                type="text"
                list="profile-timezones"
                defaultValue={Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'}
                data-testid="profile-account-timezone"
              />
              <datalist id="profile-timezones">
                <option value="UTC" />
                <option value="Asia/Bangkok" />
                <option value="America/Los_Angeles" />
                <option value="Europe/London" />
              </datalist>
            </label>
            <button
              className={styles.primary}
              type="submit"
              disabled={saving || language === user?.locale}
              data-testid="profile-account-save"
            >
              {saving ? t('profile.saving') : t('profile.account.save')}
            </button>
          </form>
        </section>
        <section
          className={styles.card}
          aria-labelledby="profile-password-title"
          data-testid="profile-password-card"
        >
          <h2 id="profile-password-title">{t('profile.password.title')}</h2>
          <ul className={styles.muted} data-testid="profile-password-rules">
            <li>{t('auth.validation.password-short', { min: 8 })}</li>
            <li>{t('auth.validation.password-long', { max: 128 })}</li>
            <li>{t('profile.password.rules.email')}</li>
          </ul>
          {passwordError && (
            <div className={styles.error} role="alert" data-testid="profile-password-error">
              {passwordError}
            </div>
          )}
          <form onSubmit={(event) => void changePassword(event)}>
            <label className={styles.field}>
              {t('profile.password.current')}
              <input
                type="password"
                autoComplete="current-password"
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
                data-testid="profile-password-current"
              />
            </label>
            <label className={styles.field}>
              {t('profile.password.new')}
              <input
                type="password"
                autoComplete="new-password"
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
                data-testid="profile-password-new"
              />
            </label>
            <label className={styles.field}>
              {t('profile.password.confirm')}
              <input
                type="password"
                autoComplete="new-password"
                value={confirmPassword}
                onChange={(event) => setConfirmPassword(event.target.value)}
                data-testid="profile-password-confirm"
              />
            </label>
            <p className={styles.muted}>{t('profile.password.help')}</p>
            <button
              className={styles.primary}
              type="submit"
              disabled={passwordBusy || rateLimitedUntil > 0}
              data-testid="profile-password-submit"
            >
              {passwordBusy ? t('profile.saving') : t('profile.password.submit')}
            </button>
          </form>
        </section>
        <section
          className={styles.card}
          aria-labelledby="profile-sessions-title"
          data-testid="profile-sessions-card"
        >
          <div className={styles.cardHead}>
            <h2 id="profile-sessions-title" ref={sessionsHeading} tabIndex={-1}>
              {t('profile.sessions.title')}
            </h2>
            <button
              className={styles.secondary}
              type="button"
              onClick={(event) => {
                confirmationTrigger.current = event.currentTarget;
                setConfirmAction({ kind: 'others' });
              }}
              disabled={sessionsLoading || sessions.filter((item) => !item.current).length === 0}
              data-testid="profile-sessions-revoke-others"
            >
              {t('profile.sessions.revoke-others')}
            </button>
          </div>
          {sessionsLoading ? (
            <div
              className={styles.skeleton}
              aria-busy="true"
              data-testid="profile-skeleton-sessions"
            >
              <i />
              <i />
            </div>
          ) : sessionsError && !sessionsHasLoaded ? (
            <div className={styles.error} role="alert" data-testid="profile-error-sessions">
              {t('profile.error.load', { card: t('profile.sessions.title') })}
              <button
                className={styles.secondary}
                onClick={() => void loadSessions()}
                data-testid="profile-error-retry-sessions"
              >
                {t('common.retry')}
              </button>
            </div>
          ) : (
            <>
              {sessionsError && (
                <p className={styles.muted} role="status" data-testid="profile-stale-note">
                  {t('profile.stale', { time: new Date(sessionsUpdatedAt).toLocaleString(locale) })}{' '}
                  <button className={styles.secondary} onClick={() => void loadSessions()}>
                    {t('common.retry')}
                  </button>
                </p>
              )}
              {sessions.length === 0 ? (
                <p className={styles.muted}>{t('profile.sessions.empty')}</p>
              ) : (
                <ul className={styles.list}>
                  {sessions.map((item) => (
                    <li key={item.id} data-testid={`profile-session-row-${item.id}`}>
                      <div>
                        <strong>
                          {item.user_agent_label || t('profile.sessions.unknown-device')}
                        </strong>
                        <p>
                          {item.ip_masked} ·{' '}
                          {t('profile.sessions.active', {
                            time: new Date(item.last_seen_at).toLocaleString(locale),
                          })}
                        </p>
                      </div>
                      {item.current ? (
                        <span className={styles.pill} data-testid="profile-session-current">
                          {t('profile.sessions.current')}
                        </span>
                      ) : (
                        <button
                          className={styles.secondary}
                          onClick={(event) => {
                            confirmationTrigger.current = event.currentTarget;
                            setConfirmAction({ kind: 'one', session: item });
                          }}
                          aria-label={t('profile.sessions.revoke-label', {
                            device: item.user_agent_label || t('profile.sessions.unknown-device'),
                          })}
                          data-testid={`profile-session-revoke-${item.id}`}
                        >
                          {t('profile.sessions.revoke')}
                        </button>
                      )}
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </section>
      </div>
      {changedElsewhere && (
        <p className={styles.notice} role="status" data-testid="profile-changed-note">
          {t('profile.changed-elsewhere')}{' '}
          <button
            type="button"
            className={styles.secondary}
            onClick={() => window.location.reload()}
          >
            {t('common.reload')}
          </button>
        </p>
      )}
      <p className={styles.signout}>
        <button
          className={styles.secondary}
          type="button"
          onClick={() => void signOut()}
          data-testid="profile-account-signout"
        >
          {t('profile.signout')}
        </button>
      </p>
      {confirmAction && (
        <div className={styles.modalBackdrop}>
          <section
            className={styles.modal}
            role="dialog"
            aria-modal="true"
            aria-labelledby="profile-confirm-title"
            onKeyDown={onDialogKeyDown}
            data-testid="profile-confirm-dialog"
          >
            <h2 id="profile-confirm-title">
              {confirmAction.kind === 'one'
                ? t('profile.sessions.revoke.title')
                : t('profile.sessions.revoke-others')}
            </h2>
            <p>
              {confirmAction.kind === 'one'
                ? t('profile.sessions.revoke.text', {
                    device:
                      confirmAction.session.user_agent_label ||
                      t('profile.sessions.unknown-device'),
                  })
                : t('profile.sessions.revoke-others.text')}
            </p>
            <div className={styles.modalActions}>
              <button
                className={styles.secondary}
                type="button"
                autoFocus
                onClick={() => {
                  setConfirmAction(null);
                  requestAnimationFrame(() => confirmationTrigger.current?.focus());
                }}
                data-testid="profile-confirm-cancel"
              >
                {t('profile.confirm.cancel')}
              </button>
              <button
                className={styles.primary}
                type="button"
                onClick={() => void confirmDialog()}
                data-testid="profile-confirm-ok"
              >
                {t('profile.sessions.revoke')}
              </button>
            </div>
          </section>
        </div>
      )}
      {blocker.state === 'blocked' && (
        <div className={styles.modalBackdrop}>
          <section
            className={styles.modal}
            role="dialog"
            aria-modal="true"
            aria-labelledby="profile-unsaved-title"
            data-testid="profile-unsaved-dialog"
          >
            <h2 id="profile-unsaved-title">{t('profile.unsaved')}</h2>
            <div className={styles.modalActions}>
              <button
                className={styles.secondary}
                type="button"
                autoFocus
                onClick={() => blocker.reset()}
              >
                {t('common.cancel')}
              </button>
              <button className={styles.primary} type="button" onClick={() => blocker.proceed()}>
                {t('profile.unsaved.leave')}
              </button>
            </div>
          </section>
        </div>
      )}
    </div>
  );
}
