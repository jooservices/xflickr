import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';
import { useI18n } from '../../shared';
import { useTheme } from '../../shared/theme';
import { useSession } from '../../entities';
import { useQueryClient } from '@tanstack/react-query';
import { SESSION_QUERY_KEY } from '../../entities/session/SessionProvider';
import { fetchCsrfConfig, request, validatedReturnTo } from '../../features/auth/api';
import type { AuthUser } from '../../features/auth/api';
import styles from '../auth.module.css';

export function RegisterPage() {
  const { t, locale, toggleLocale } = useI18n();
  const { theme, toggleTheme } = useTheme('user');
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { session } = useSession();
  const params = new URLSearchParams(window.location.search);
  const returnTo = validatedReturnTo(params.get('return_to'));
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [terms, setTerms] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [config, setConfig] = useState<{
    registration_open: boolean;
    terms_url?: string;
    privacy_url?: string;
  } | null>(null);
  const [partial, setPartial] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    document.title = `${t('auth.register.title')} · XFlickr`;
  }, [t]);

  useEffect(() => {
    let active = true;
    void fetchCsrfConfig()
      .then((value) => {
        if (active) setConfig(value);
      })
      .catch(() => {
        if (active) {
          setConfig({ registration_open: true });
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
    if (!email.trim() || !email.includes('@')) {
      setError(t('auth.validation.email-invalid'));
      return;
    }
    if (password.length < 8) {
      setError(t('auth.validation.password-short', { min: 8 }));
      return;
    }
    if (password.length > 128) {
      setError(t('auth.validation.password-long', { max: 128 }));
      return;
    }
    if (password.toLowerCase() === email.trim().toLowerCase()) {
      setError(t('auth.password.rule-email'));
      return;
    }
    if (confirm !== password) {
      setError(t('auth.validation.confirm-mismatch'));
      return;
    }
    if (!terms) {
      setError(t('auth.validation.terms-required'));
      return;
    }
    setBusy(true);
    try {
      const created = await request<AuthUser>('/auth/register', 'POST', {
        email: email.trim(),
        password,
        accept_terms: true,
        locale,
      });
      if (created.status === 409) {
        setError(t('auth.error.email-taken'));
        return;
      }
      if (created.status !== 201) {
        setError(t('auth.error.server'));
        return;
      }
      const loggedIn = await request<AuthUser>('/auth/login', 'POST', {
        email: email.trim(),
        password,
      });
      setPassword('');
      setConfirm('');
      if (loggedIn.status === 200) {
        queryClient.setQueryData(SESSION_QUERY_KEY, {
          status: 'authenticated',
          user: loggedIn.data,
        });
        localStorage.setItem('xf.signed-in', String(Date.now()));
        void navigate('/onboarding', { replace: true });
      } else {
        void navigate(
          `/login?created=1${returnTo === '/' ? '' : `&return_to=${encodeURIComponent(returnTo)}`}`,
          { replace: true },
        );
      }
    } catch {
      setError(t('auth.error.server'));
    } finally {
      setBusy(false);
    }
  }

  const lengthMet = password.length >= 8 && password.length <= 128;
  const emailMet = password.length > 0 && password.toLowerCase() !== email.trim().toLowerCase();
  if (config?.registration_open === false)
    return (
      <main className={styles.page} data-role="user" data-theme={theme} data-testid="auth-page">
        <header className={styles.header}>
          <Link to="/" className={styles.brand}>
            XFlickr
          </Link>
          <div className={styles.headerActions}>
            <button
              className={styles.textButton}
              onClick={toggleLocale}
              data-testid="auth-lang-toggle"
            >
              {locale.toUpperCase()}
            </button>
            <button
              className={styles.iconButton}
              onClick={toggleTheme}
              data-testid="auth-theme-toggle"
            >
              ◐
            </button>
          </div>
        </header>
        <section className={styles.card} data-testid="auth-register-closed">
          <h1>{t('auth.register.closed.title')}</h1>
          <p>{t('auth.register.closed.text')}</p>
          <Link className={styles.primary} to="/login" data-testid="auth-register-closed-cta">
            {t('auth.register.closed.cta')}
          </Link>
        </section>
      </main>
    );

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
          <h1 data-testid="auth-register-title">{t('auth.register.title')}</h1>
          <p className={styles.intro}>{t('auth.register.intro')}</p>
          {partial && (
            <p className={styles.muted} data-testid="auth-partial-note">
              {t('auth.config.partial')}
            </p>
          )}
          {error && (
            <div className={styles.banner} role="alert" data-testid="auth-error-banner">
              {error}
            </div>
          )}
          <form
            onSubmit={(event) => void submit(event)}
            noValidate
            data-testid="auth-register-form"
          >
            <label className={styles.field}>
              {t('auth.field.email')} <span>{t('auth.field.required')}</span>
              <input
                type="email"
                autoComplete="username"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                data-testid="auth-register-email"
              />
            </label>
            <label className={styles.field}>
              {t('auth.field.password')} <span>{t('auth.field.required')}</span>
              <div className={styles.password}>
                <input
                  type={showPassword ? 'text' : 'password'}
                  autoComplete="new-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  data-testid="auth-register-password"
                />
                <button
                  type="button"
                  className={styles.reveal}
                  aria-label={t(showPassword ? 'auth.password.hide' : 'auth.password.show')}
                  aria-pressed={showPassword}
                  onClick={() => setShowPassword(!showPassword)}
                  data-testid="auth-register-password-toggle"
                >
                  ◉
                </button>
              </div>
              <ul className={styles.rules} data-testid="auth-register-rules">
                <li
                  data-testid="auth-register-rule-length"
                  aria-label={t(lengthMet ? 'auth.password.rule-met' : 'auth.password.rule-unmet')}
                >
                  {lengthMet ? '✓' : '·'} {t('auth.password.rule-length', { min: 8, max: 128 })}
                </li>
                <li
                  data-testid="auth-register-rule-email"
                  aria-label={t(emailMet ? 'auth.password.rule-met' : 'auth.password.rule-unmet')}
                >
                  {emailMet ? '✓' : '·'} {t('auth.password.rule-email')}
                </li>
              </ul>
            </label>
            <label className={styles.field}>
              {t('auth.field.confirm')} <span>{t('auth.field.required')}</span>
              <input
                type="password"
                autoComplete="new-password"
                value={confirm}
                onChange={(event) => setConfirm(event.target.value)}
                data-testid="auth-register-confirm"
              />
            </label>
            <label className={styles.check}>
              <input
                type="checkbox"
                checked={terms}
                onChange={(event) => setTerms(event.target.checked)}
                data-testid="auth-register-terms"
              />
              <span>
                {t('auth.register.terms', {
                  terms: t('auth.register.terms-link'),
                  privacy: t('auth.register.privacy-link'),
                })}
              </span>
            </label>
            {config?.terms_url !== undefined || config?.privacy_url !== undefined ? (
              <div className={styles.termsLinks}>
                {config?.terms_url === undefined ? null : (
                  <a href={config.terms_url} data-testid="auth-register-terms-link">
                    {t('auth.register.terms-link')}
                  </a>
                )}
                {config?.privacy_url === undefined ? null : (
                  <a href={config.privacy_url} data-testid="auth-register-privacy-link">
                    {t('auth.register.privacy-link')}
                  </a>
                )}
              </div>
            ) : null}
            <button
              className={styles.primary}
              type="submit"
              disabled={busy}
              data-testid="auth-register-submit"
            >
              {busy ? t('auth.register.submitting') : t('auth.register.submit')}
            </button>
          </form>
          <div className={styles.footer}>
            <Link
              to={`/login${returnTo === '/' ? '' : `?return_to=${encodeURIComponent(returnTo)}`}`}
              data-testid="auth-register-to-login"
            >
              {t('auth.register.to-login')}
            </Link>
          </div>
        </section>
      </div>
    </main>
  );
}
