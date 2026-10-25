import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import { useQueryClient } from '@tanstack/react-query';
import { useI18n } from '../../shared';
import { useTheme } from '../../shared/theme';
import { fetchCsrfConfig, request, validatedReturnTo } from '../../features/auth/api';
import type { AuthUser } from '../../features/auth/api';
import { SESSION_QUERY_KEY } from '../../entities/session/SessionProvider';
import { useSession } from '../../entities';
import styles from '../auth.module.css';

export function LoginPage() {
  const { t, locale, toggleLocale } = useI18n();
  const { theme, toggleTheme } = useTheme('user');
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { session } = useSession();
  const params = new URLSearchParams(location.search);
  const expired = params.get('reason') === 'expired';
  const returnTo = validatedReturnTo(params.get('return_to'));
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [busy, setBusy] = useState(false);
  const [config, setConfig] = useState<{
    registration_open: boolean;
    passkey_enabled: boolean;
  } | null>(null);
  const [partial, setPartial] = useState(false);
  const [error, setError] = useState('');
  const [rateLimited, setRateLimited] = useState(false);

  useEffect(() => {
    document.title = `${t(expired ? 'auth.expired.heading' : 'auth.login.title')} · XFlickr`;
  }, [expired, t]);

  useEffect(() => {
    let active = true;
    void fetchCsrfConfig()
      .then((value) => {
        if (active) setConfig(value);
      })
      .catch(() => {
        if (active) {
          setConfig({ registration_open: true, passkey_enabled: false });
          setPartial(true);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (session.status === 'authenticated') {
      void navigate(session.user.role === 'admin' ? '/admin' : returnTo, { replace: true });
    }
  }, [navigate, returnTo, session]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError('');
    setRateLimited(false);
    if (!email.trim() || !email.includes('@')) {
      setError(t('auth.validation.email-invalid'));
      return;
    }
    if (!password) {
      setError(t('auth.validation.password-required'));
      return;
    }
    setBusy(true);
    try {
      const response = await request<AuthUser>('/auth/login', 'POST', {
        email: email.trim(),
        password,
      });
      if (response.status === 200) {
        setPassword('');
        queryClient.setQueryData(SESSION_QUERY_KEY, {
          status: 'authenticated',
          user: response.data,
        });
        localStorage.setItem('xf.signed-in', String(Date.now()));
        document.documentElement.lang = response.data.locale || locale;
        void navigate(response.data.role === 'admin' && returnTo === '/' ? '/admin' : returnTo, {
          replace: true,
        });
      } else if (response.status === 429) {
        setRateLimited(true);
        setError(t('auth.error.rate-limited', { minutes: 1 }));
        setPassword('');
      } else if (response.status === 423) {
        setError(t('auth.error.locked', { time: '' }));
        setPassword('');
      } else if (response.status === 403) {
        setError(t('auth.error.disabled'));
        setPassword('');
      } else if (response.status === 401) {
        setError(t('auth.error.invalid-credentials'));
        setPassword('');
      } else {
        setError(t('auth.error.server'));
      }
    } catch {
      setError(t('auth.error.server'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className={styles.page} data-role="user" data-theme={theme} data-testid="auth-page">
      <header className={styles.header} data-testid="auth-header">
        <Link to="/" className={styles.brand} data-testid="auth-brand">
          <span className={styles.dots} aria-hidden="true">
            <i />
            <i />
          </span>
          XFlickr
        </Link>
        <div className={styles.headerActions}>
          <button
            type="button"
            className={styles.textButton}
            onClick={toggleLocale}
            aria-label={t('auth.header.language')}
            data-testid="auth-lang-toggle"
          >
            {locale.toUpperCase()}
          </button>
          <button
            type="button"
            className={styles.iconButton}
            onClick={toggleTheme}
            aria-label={t('auth.header.theme')}
            data-testid="auth-theme-toggle"
          >
            {theme === 'dark' ? '☀' : '◐'}
          </button>
        </div>
      </header>
      <div className={styles.layout}>
        <aside className={styles.hero} aria-hidden="true" data-testid="auth-hero">
          <div className={styles.mosaic}>
            <span />
            <span />
            <span />
            <span />
            <span />
            <span />
          </div>
          <p>{t('auth.hero.title')}</p>
        </aside>
        <section className={styles.card} data-testid="auth-card">
          <h1 data-testid="auth-login-title">
            {t(expired ? 'auth.expired.heading' : 'auth.login.title')}
          </h1>
          <p className={styles.intro}>{t(expired ? 'auth.expired.text' : 'auth.login.intro')}</p>
          {expired && (
            <div className={styles.info} role="status" data-testid="auth-expired-banner">
              <strong>{t('auth.expired.title')}</strong>
              <p>{t('auth.expired.text')}</p>
              <button
                type="button"
                className={styles.dismiss}
                aria-label={t('common.close')}
                onClick={() =>
                  void navigate(`/login?return_to=${encodeURIComponent(returnTo)}`, {
                    replace: true,
                  })
                }
                data-testid="auth-expired-dismiss"
              >
                ×
              </button>
            </div>
          )}
          {error && (
            <div
              className={`${styles.banner} ${rateLimited ? styles.warning : ''}`}
              role="alert"
              tabIndex={-1}
              data-testid={rateLimited ? 'auth-ratelimit-banner' : 'auth-error-banner'}
            >
              {error}
              {!rateLimited && (
                <button
                  type="button"
                  className={styles.linkButton}
                  onClick={() => void fetchCsrfConfig().catch(() => undefined)}
                  data-testid="auth-error-retry"
                >
                  {t('common.retry')}
                </button>
              )}
            </div>
          )}
          {partial && (
            <p className={styles.muted} data-testid="auth-partial-note">
              {t('auth.config.partial')}
            </p>
          )}
          <form onSubmit={(event) => void submit(event)} noValidate data-testid="auth-login-form">
            <label className={styles.field}>
              {t('auth.field.email')} <span>{t('auth.field.required')}</span>
              <input
                type="email"
                autoComplete="username"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                aria-invalid={Boolean(error && error.includes('@'))}
                data-testid="auth-login-email"
              />
            </label>
            <label className={styles.field}>
              {t('auth.field.password')} <span>{t('auth.field.required')}</span>
              <div className={styles.password}>
                <input
                  type={showPassword ? 'text' : 'password'}
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  data-testid="auth-login-password"
                />
                <button
                  type="button"
                  className={styles.reveal}
                  aria-label={t(showPassword ? 'auth.password.hide' : 'auth.password.show')}
                  aria-pressed={showPassword}
                  onClick={() => setShowPassword(!showPassword)}
                  data-testid="auth-login-password-toggle"
                >
                  ◉
                </button>
              </div>
            </label>
            <button
              className={styles.primary}
              type="submit"
              disabled={busy || rateLimited}
              data-testid="auth-login-submit"
            >
              {busy
                ? t('auth.login.submitting')
                : t(expired ? 'auth.expired.submit' : 'auth.login.submit')}
            </button>
          </form>
          {config?.passkey_enabled && typeof PublicKeyCredential !== 'undefined' && (
            <>
              <div className={styles.divider}>{t('auth.login.or')}</div>
              <button
                className={styles.secondary}
                type="button"
                disabled={busy}
                data-testid="auth-login-passkey"
              >
                {t('auth.login.passkey')}
              </button>
            </>
          )}
          <div className={styles.footer}>
            {expired ? (
              <Link to="/login" data-testid="auth-expired-other-account">
                {t('auth.expired.other-account')}
              </Link>
            ) : (
              config?.registration_open !== false && (
                <Link
                  to={`/register${returnTo === '/' ? '' : `?return_to=${encodeURIComponent(returnTo)}`}`}
                  data-testid="auth-login-to-register"
                >
                  {t('auth.login.to-register')}
                </Link>
              )
            )}
          </div>
        </section>
      </div>
    </main>
  );
}
