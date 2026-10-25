import { useEffect, useRef, useState } from 'react';
import type { ReactNode, RefObject } from 'react';
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router';
import { writeRoleView } from '../guards';
import { useSession } from '../../entities';
import { ErrorState, Icon, useI18n, useTheme } from '../../shared';
import { ClusterFooter } from './ClusterFooter';
import { LangToggle, LiveToggle, ThemeToggle } from './Controls';
import { Notifications } from './Notifications';
import { useAdminShell } from './data';
import { ADMIN_NAV_GROUPS } from './nav';
import { ShellRuntimeProvider, useShellRuntime } from './runtime';
import { useAge } from './useAge';
import { useMediaQuery } from './useMediaQuery';
import { useOverlay } from './useOverlay';
import { useRouteFocus } from './useRouteFocus';
import styles from './shells.module.css';

type TopBarProps = {
  theme: 'light' | 'dark';
  onToggleTheme: () => void;
  /** Present only when the viewer is an admin. */
  showRoleSwitch: boolean;
  bellCount?: number;
  drawerOpen?: boolean;
  onMenu?: () => void;
  menuRef?: RefObject<HTMLButtonElement | null>;
  email?: string;
};

function AdminTopBar({
  theme,
  onToggleTheme,
  showRoleSwitch,
  bellCount,
  drawerOpen = false,
  onMenu,
  menuRef,
  email,
}: TopBarProps) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const initials = (email?.split('@')[0] ?? '').slice(0, 2).toUpperCase();

  return (
    <header className={styles.topbar} data-testid="admin-topbar">
      <div className={styles.topRow}>
        {onMenu === undefined ? null : (
          <button
            ref={menuRef}
            type="button"
            className={`${styles.iconBtn} ${styles.menuBtn}`}
            aria-label={drawerOpen ? t('shell.menu.close') : t('shell.menu.open')}
            aria-expanded={drawerOpen}
            onClick={onMenu}
            data-testid="admin-menu-btn"
          >
            <Icon name="menu" />
          </button>
        )}
        <Link to="/admin" className={styles.brand} data-testid="admin-brand">
          <span className={styles.dots} aria-hidden="true">
            <i />
            <i />
          </span>
          {t('shell.brand')}
        </Link>
        {showRoleSwitch ? (
          <div
            className={styles.roleSwitch}
            role="group"
            aria-label={t('shell.role.group')}
            data-testid="admin-role-switch"
          >
            <button
              type="button"
              aria-pressed="false"
              onClick={() => {
                writeRoleView('user');
                void navigate('/');
              }}
            >
              <Icon name="user" size={14} />
              <span className={styles.label}>{t('shell.role.user')}</span>
            </button>
            <button type="button" aria-pressed="true">
              <Icon name="shield" size={14} />
              <span className={styles.label}>{t('shell.role.admin')}</span>
            </button>
          </div>
        ) : null}
        <div className={styles.topActions}>
          <LiveToggle testId="admin-live-toggle" />
          <LangToggle testId="admin-lang-toggle" />
          <ThemeToggle testId="admin-theme-toggle" theme={theme} onToggle={onToggleTheme} />
          {showRoleSwitch ? (
            <>
              <Notifications
                count={bellCount}
                buttonTestId="admin-bell"
                countTestId="admin-bell-count"
              />
              <Link to="/admin/users" className={`${styles.iconBtn} ${styles.hideSm}`}>
                <span className={styles.avatar} aria-hidden="true">
                  {initials}
                </span>
                <span className={styles.srOnly}>{t('shell.profile')}</span>
              </Link>
            </>
          ) : null}
        </div>
      </div>
    </header>
  );
}

function SkipLink() {
  const { t } = useI18n();

  return (
    <a className={styles.skip} href="#main" data-testid="shell-skip-link">
      {t('shell.skip')}
    </a>
  );
}

/** What the guard shows while `role` is unknown: the top bar and a spinner, never the rail. */
export function AdminGuardLoading() {
  const { t } = useI18n();
  const { theme, toggleTheme } = useTheme('admin');

  return (
    <ShellRuntimeProvider>
      <div className={styles.root} data-role="admin" data-theme={theme} data-testid="admin-shell">
        <SkipLink />
        <AdminTopBar theme={theme} onToggleTheme={toggleTheme} showRoleSwitch={false} />
        <main id="main" className={styles.centered} tabIndex={-1} data-testid="admin-main">
          <p role="status" className={styles.spinnerLine}>
            <span className={styles.spinner} aria-hidden="true" />
            {t('admin.shell.loading.spinner')}
          </p>
        </main>
      </div>
    </ShellRuntimeProvider>
  );
}

/** `GET /me` failed with a non-401 error: banner with Retry, skeleton below. */
export function AdminGuardFailed() {
  const { t } = useI18n();
  const { retry } = useSession();
  const { theme, toggleTheme } = useTheme('admin');

  return (
    <ShellRuntimeProvider>
      <div className={styles.root} data-role="admin" data-theme={theme} data-testid="admin-shell">
        <SkipLink />
        <AdminTopBar theme={theme} onToggleTheme={toggleTheme} showRoleSwitch={false} />
        <main id="main" className={styles.mainPlain} tabIndex={-1} data-testid="admin-main">
          <ErrorState
            testId="admin"
            title={t('admin.shell.me-error.title')}
            text={t('admin.shell.me-error.text')}
            onRetry={retry}
            withSkeleton
          />
        </main>
      </div>
    </ShellRuntimeProvider>
  );
}

function RailItem({
  path,
  label,
  id,
  icon,
  issues,
  onNavigate,
}: {
  path: string;
  label: string;
  id: string;
  icon: Parameters<typeof Icon>[0]['name'];
  issues: number;
  onNavigate: () => void;
}) {
  const { t } = useI18n();
  const showBadge = id === 'issues' && issues > 0;

  return (
    <NavLink
      to={path}
      end={path === '/admin'}
      className={styles.navItem}
      onClick={onNavigate}
      data-testid={`admin-nav-${id}`}
    >
      <Icon name={icon} size={16} />
      {showBadge ? (
        <>
          <span aria-hidden="true">{label}</span>
          <span className={styles.srOnly}>{t('admin.nav.issues-count', { count: issues })}</span>
          <span className={styles.badge} aria-hidden="true" data-testid="admin-nav-issues-badge">
            {issues}
          </span>
        </>
      ) : (
        <span>{label}</span>
      )}
    </NavLink>
  );
}

function Frame() {
  const { t } = useI18n();
  const { session } = useSession();
  const { theme, toggleTheme } = useTheme('admin');
  const { liveOff } = useShellRuntime();
  const location = useLocation();
  const compact = useMediaQuery('(max-width: 900px)');
  const [drawerOpen, setDrawerOpen] = useState(false);
  const shell = useAdminShell(true);
  const age = useAge(shell.dataUpdatedAt);
  const modal = compact && drawerOpen;
  const closeDrawer = () => {
    setDrawerOpen(false);
  };
  const menuRef = useRef<HTMLButtonElement>(null);
  const railRef = useOverlay<HTMLElement>({
    open: modal,
    onClose: closeDrawer,
    trap: true,
    openerRef: menuRef,
  });
  const email = session.status === 'authenticated' ? session.user.email : undefined;
  useRouteFocus();

  useEffect(() => {
    writeRoleView('admin');
  }, []);

  const data = shell.data;
  const footerState = shell.isPending ? 'loading' : shell.isError ? 'error' : 'ready';
  const showCounts = footerState === 'ready' && data !== undefined;

  return (
    <div className={styles.root} data-role="admin" data-theme={theme} data-testid="admin-shell">
      <SkipLink />
      <div inert={modal}>
        <AdminTopBar
          theme={theme}
          onToggleTheme={toggleTheme}
          showRoleSwitch
          bellCount={showCounts ? data.unread_notifications : undefined}
          drawerOpen={drawerOpen}
          menuRef={menuRef}
          onMenu={() => {
            setDrawerOpen(!drawerOpen);
          }}
          email={email}
        />
      </div>
      <div className={styles.adminGrid}>
        <nav
          ref={railRef}
          className={`${styles.rail} ${drawerOpen ? styles.railOpen : ''}`}
          aria-label={t('admin.nav.label')}
          {...(modal ? { role: 'dialog', 'aria-modal': true } : {})}
          data-testid="admin-rail"
        >
          {ADMIN_NAV_GROUPS.map((group) => (
            <div
              key={group.id}
              className={styles.navGroup}
              role="group"
              aria-labelledby={`admin-group-${group.id}`}
            >
              <span id={`admin-group-${group.id}`} className={styles.eyebrow}>
                {t(group.labelKey)}
              </span>
              {group.items.map((item) => (
                <RailItem
                  key={item.id}
                  id={item.id}
                  path={item.path}
                  icon={item.icon}
                  label={t(item.labelKey)}
                  issues={showCounts ? data.open_issues : 0}
                  onNavigate={closeDrawer}
                />
              ))}
            </div>
          ))}
          <ClusterFooter
            state={footerState}
            cluster={data?.cluster}
            ageMs={age}
            liveOff={liveOff}
            onRetry={() => {
              void shell.refetch();
            }}
          />
        </nav>
        {drawerOpen && compact ? (
          <button
            type="button"
            className={styles.railScrim}
            aria-label={t('shell.menu.close')}
            tabIndex={-1}
            onClick={closeDrawer}
            data-testid="admin-rail-scrim"
          />
        ) : null}
        <main
          id="main"
          className={styles.main}
          tabIndex={-1}
          inert={modal}
          data-testid="admin-main"
        >
          <Outlet key={location.pathname} />
        </main>
      </div>
    </div>
  );
}

/** Layout route for every path under /admin: top bar, 14-item rail, cluster footer, content. */
export function AdminShell(): ReactNode {
  return (
    <ShellRuntimeProvider>
      <Frame />
    </ShellRuntimeProvider>
  );
}
