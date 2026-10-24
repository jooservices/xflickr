import { useQueryClient } from '@tanstack/react-query';
import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { Link, NavLink, Navigate, Outlet, useLocation, useMatch, useNavigate } from 'react-router';
import { markBootHandled, shouldRedirectAdminOnBoot } from '../guards';
import { useSession } from '../../entities';
import {
  ErrorState,
  ForbiddenState,
  Icon,
  LoadingState,
  useI18n,
  useLoadingTiming,
  useTheme,
} from '../../shared';
import { AvatarMenu } from './AvatarMenu';
import { Banners } from './Banners';
import { LangToggle, LiveToggle, ThemeToggle } from './Controls';
import { SessionModal, ShortcutsDialog } from './Dialogs';
import { MaintenancePage, useMaintenance } from './Maintenance';
import { Notifications } from './Notifications';
import { Tray } from './Tray';
import { useShellSummary } from './data';
import { USER_NAV } from './nav';
import { ShellRuntimeProvider, useShellRuntime } from './runtime';
import { useRouteFocus } from './useRouteFocus';
import styles from './shells.module.css';

type UserShellProps = {
  /** Renders the 403 page in the content area (a non-admin on an admin route). */
  forbidden?: boolean;
};

function Brand({ to, testId }: { to: string; testId: string }) {
  const { t } = useI18n();

  return (
    <Link to={to} className={styles.brand} data-testid={testId}>
      <span className={styles.dots} aria-hidden="true">
        <i />
        <i />
      </span>
      {t('shell.brand')}
    </Link>
  );
}

function SearchBox({ disabled }: { disabled: boolean }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const inputRef = useRef<HTMLInputElement>(null);
  const initial =
    location.pathname === '/search' ? (new URLSearchParams(location.search).get('q') ?? '') : '';

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target;
      const typing =
        target instanceof HTMLElement &&
        (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName));
      if (event.key === '/' && !typing && !event.metaKey && !event.ctrlKey) {
        event.preventDefault();
        inputRef.current?.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, []);

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const text = inputRef.current?.value.trim() ?? '';
    if (text === '') {
      inputRef.current?.focus();
      return;
    }
    void navigate(`/search?q=${encodeURIComponent(text)}`);
  };

  return (
    <form className={styles.search} role="search" onSubmit={submit} data-testid="shell-search">
      <Icon name="search" />
      <input
        key={initial}
        ref={inputRef}
        type="search"
        defaultValue={initial}
        disabled={disabled}
        aria-label={t('shell.search.label')}
        placeholder={t('shell.search.placeholder')}
        data-testid="shell-search-input"
      />
    </form>
  );
}

function Crumb() {
  const { t } = useI18n();
  const contact = useMatch('/contacts/:id');
  const album = useMatch('/albums/:id');
  const location = useLocation();
  if (contact === null && album === null) return null;

  const from = (location.state as { from?: { path?: unknown; name?: unknown } } | null)?.from;
  const hasFrom = typeof from?.path === 'string' && typeof from.name === 'string';
  const to = hasFrom ? from.path : contact === null ? '/albums' : '/contacts';
  const label = hasFrom
    ? from.name
    : t(contact === null ? 'shell.nav.albums' : 'shell.nav.contacts');

  return (
    <Link
      to={String(to)}
      className={styles.crumb}
      aria-label={t('shell.crumb.back-to', { name: String(label) })}
      data-testid="shell-crumb"
    >
      <span aria-hidden="true">‹ </span>
      {String(label)}
    </Link>
  );
}

function Shell({ forbidden }: UserShellProps) {
  const { t } = useI18n();
  const { session, retry } = useSession();
  const { theme, toggleTheme } = useTheme('user');
  const { offline } = useShellRuntime();
  const navigate = useNavigate();
  const location = useLocation();
  const client = useQueryClient();
  const [shortcuts, setShortcuts] = useState(false);
  const authenticated = session.status === 'authenticated';
  const summary = useShellSummary(authenticated);
  const maintenance = useMaintenance(authenticated);
  const { toggleLive } = useShellRuntime();
  const loading = session.status === 'loading';
  const showSkeleton = useLoadingTiming(loading);
  const onboarding = location.pathname === '/onboarding';
  useRouteFocus();

  // `l` toggles Live; `g` then `h` goes home; `?` opens the shortcut list.
  const lastKey = useRef<{ key: string; at: number } | null>(null);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const target = event.target;
      if (
        (target instanceof HTMLElement &&
          (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))) ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey
      ) {
        return;
      }
      const now = Date.now();
      if (event.key === 'l') toggleLive();
      if (event.key === '?') setShortcuts(true);
      if (event.key === 'h' && lastKey.current?.key === 'g' && now - lastKey.current.at < 1500) {
        void navigate('/');
      }
      lastKey.current = { key: event.key, at: now };
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [navigate, toggleLive]);

  if (session.status === 'authenticated' && maintenance.down) {
    return <MaintenancePage checkedAt={maintenance.checkedAt} onRetry={maintenance.retryNow} />;
  }

  const user = session.status === 'authenticated' ? session.user : undefined;
  const data = summary.data;
  const displayName = user?.email.split('@')[0] ?? '';
  const ready = authenticated;

  const signIn = () => {
    const returnTo = location.pathname + location.search;
    window.location.assign(`/login?return_to=${encodeURIComponent(returnTo)}`);
  };

  const signOut = () => {
    void fetch('/auth/logout', { method: 'POST', credentials: 'same-origin' }).finally(() => {
      client.clear();
      window.location.assign('/login');
    });
  };

  let content;
  if (session.status === 'error') {
    content = (
      <ErrorState
        testId="shell"
        title={t('shell.error.title')}
        text={t('shell.error.text')}
        onRetry={retry}
        withSkeleton
      />
    );
  } else if (loading) {
    content = <LoadingState testId="shell" label={t('shell.loading')} skeleton={showSkeleton} />;
  } else if (forbidden === true) {
    content = (
      <div data-testid="admin-forbidden">
        <ForbiddenState
          testId="shell-forbidden"
          ctaTestId="admin-forbidden-cta"
          onHome={() => {
            void navigate('/');
          }}
        />
      </div>
    );
  } else {
    content = <Outlet key={location.pathname} />;
  }

  return (
    <div className={styles.root} data-role="user" data-theme={theme}>
      <a className={styles.skip} href="#main" data-testid="shell-skip-link">
        {t('shell.skip')}
      </a>
      <header className={styles.topbar} data-testid="shell-topbar">
        <div className={styles.topRow}>
          <Brand to="/" testId="shell-brand" />
          <SearchBox disabled={!ready} />
          <LiveToggle testId="shell-live-toggle" />
          <LangToggle testId="shell-lang-toggle" />
          <ThemeToggle testId="shell-theme-toggle" theme={theme} onToggle={toggleTheme} />
          {ready ? (
            <>
              <Tray
                count={data?.tray_count}
                onDownload={() => {
                  void navigate('/downloads');
                }}
              />
              <Notifications
                count={data?.unread_notifications}
                buttonTestId="shell-notifications-button"
                countTestId="shell-notifications-count"
              />
              <AvatarMenu
                name={displayName}
                email={user?.email ?? ''}
                onShortcuts={() => {
                  setShortcuts(true);
                }}
                onSignOut={signOut}
              />
            </>
          ) : (
            <span className={styles.avatarSkeleton} aria-hidden="true" />
          )}
        </div>
        {onboarding ? null : (
          <nav className={styles.tabs} aria-label={t('shell.nav.label')} data-testid="shell-nav">
            {USER_NAV.map((item) =>
              ready ? (
                <NavLink
                  key={item.id}
                  to={item.path}
                  end={item.path === '/'}
                  data-testid={`shell-nav-${item.id}`}
                  ref={(node) => {
                    if (node?.getAttribute('aria-current') === 'page') {
                      node.scrollIntoView?.({ block: 'nearest', inline: 'center' });
                    }
                  }}
                >
                  {t(item.labelKey)}
                  {item.id === 'downloads' && (data?.running_downloads ?? 0) > 0 ? (
                    <span className={styles.tabCount} data-testid="shell-nav-downloads-count">
                      {data?.running_downloads}
                    </span>
                  ) : null}
                </NavLink>
              ) : (
                <span
                  key={item.id}
                  className={styles.tabDisabled}
                  aria-disabled="true"
                  data-testid={`shell-nav-${item.id}`}
                >
                  {t(item.labelKey)}
                </span>
              ),
            )}
          </nav>
        )}
      </header>
      <main id="main" className={styles.main} tabIndex={-1} data-testid="shell-main">
        <Banners
          offline={offline}
          attention={data?.attention ?? []}
          onReconnect={(item) => {
            if (item.kind === 'cloud_expired') void navigate('/sync');
            else {
              void fetch('/flickr-accounts/connect', { method: 'POST' })
                .then((response) => response.json() as Promise<{ authorize_url?: string }>)
                .then((body) => {
                  if (body.authorize_url !== undefined) window.location.assign(body.authorize_url);
                });
            }
          }}
        />
        <Crumb />
        {content}
      </main>
      {session.status === 'unauthenticated' ? <SessionModal onSignIn={signIn} /> : null}
      {shortcuts ? (
        <ShortcutsDialog
          onClose={() => {
            setShortcuts(false);
          }}
        />
      ) : null}
    </div>
  );
}

/** Layout route for every USER route: top bar, tab row and `<main id="main">`. */
export function UserShell(props: UserShellProps) {
  const { session } = useSession();

  useEffect(() => {
    if (session.status !== 'loading') markBootHandled();
  }, [session.status]);

  if (props.forbidden !== true && shouldRedirectAdminOnBoot(session)) {
    return <Navigate to="/admin" replace />;
  }

  return (
    <ShellRuntimeProvider>
      <Shell {...props} />
    </ShellRuntimeProvider>
  );
}
