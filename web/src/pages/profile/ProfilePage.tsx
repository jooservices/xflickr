import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Link } from 'react-router';
import { useSession } from '../../entities';
import { SESSION_QUERY_KEY } from '../../entities/session/SessionProvider';
import { useI18n } from '../../shared';
import type { Locale } from '../../shared/i18n/catalog';
import { request } from '../../features/auth/api';
import styles from './ProfilePage.module.css';

type SessionItem = {
  id: string;
  current: boolean;
  user_agent_label: string;
  ip_masked: string;
  last_seen_at: string;
  created_at: string;
};

export function ProfilePage() {
  const { session } = useSession();
  const { t, locale, setLocale } = useI18n();
  const client = useQueryClient();
  const user = session.status === 'authenticated' ? session.user : undefined;
  const [language, setLanguage] = useState<Locale>(locale);
  const [sessions, setSessions] = useState<SessionItem[]>([]);
  const [sessionsLoading, setSessionsLoading] = useState(true);
  const [sessionsError, setSessionsError] = useState(false);
  const [saving, setSaving] = useState(false);
  const [passwordBusy, setPasswordBusy] = useState(false);
  const displayName = user?.email.split('@')[0] ?? '';
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [message, setMessage] = useState('');
  const [passwordError, setPasswordError] = useState('');
  const title = t('profile.title');

  useEffect(() => {
    document.title = t('shell.document-title', { view: title });
  }, [t, title]);

  async function loadSessions() {
    try {
      const result = await request<{ items?: SessionItem[] }>('/me/sessions');
      if (result.status !== 200) throw new Error('Could not load sessions');
      setSessions(result.data.items ?? []);
      setSessionsError(false);
    } catch {
      setSessionsError(true);
    } finally {
      setSessionsLoading(false);
    }
  }

  useEffect(() => {
    let active = true;
    void request<{ items?: SessionItem[] }>('/me/sessions')
      .then((result) => {
        if (!active) return;
        if (result.status !== 200) {
          setSessionsError(true);
          return;
        }
        setSessions(result.data.items ?? []);
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
      const result = await request('/me/password', 'POST', {
        current_password: currentPassword,
        new_password: newPassword,
      });
      if (result.status === 401) {
        setPasswordError(t('profile.password.error.current'));
        setCurrentPassword('');
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

  async function revokeOthers() {
    if (!window.confirm(t('profile.confirm.sessions'))) return;
    const result = await request('/auth/sessions', 'DELETE');
    if (result.status === 204) await loadSessions();
    else setSessionsError(true);
  }

  async function revokeOne(item: SessionItem) {
    if (!window.confirm(t('profile.confirm.revoke', { device: item.user_agent_label }))) return;
    const result = await request(`/me/sessions/${encodeURIComponent(item.id)}`, 'DELETE');
    if (result.status === 204 || result.status === 404)
      setSessions((current) => current.filter((sessionItem) => sessionItem.id !== item.id));
    else setMessage(t('profile.error.action'));
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
              disabled={passwordBusy}
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
            <h2 id="profile-sessions-title">{t('profile.sessions.title')}</h2>
            <button
              className={styles.secondary}
              type="button"
              onClick={() => void revokeOthers()}
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
          ) : sessionsError ? (
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
          ) : sessions.length === 0 ? (
            <p>{t('profile.sessions.empty')}</p>
          ) : (
            <ul className={styles.list}>
              {sessions.map((item) => (
                <li key={item.id} data-testid={`profile-session-row-${item.id}`}>
                  <div>
                    <strong>{item.user_agent_label}</strong>
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
                      onClick={() => void revokeOne(item)}
                      aria-label={t('profile.sessions.revoke-label', {
                        device: item.user_agent_label,
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
        </section>
      </div>
      <p className={styles.signout}>
        <Link to="/login">{t('profile.signout')}</Link>
      </p>
    </div>
  );
}
