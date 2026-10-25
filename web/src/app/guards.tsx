import type { ReactElement } from 'react';
import { Navigate, useLocation } from 'react-router';
import { useSession } from '../entities';
import type { Session } from '../entities';

/** What a route guard decides from the session. */
export type AccessDecision = 'loading' | 'error' | 'login' | 'allow' | 'forbidden';

/** Pure rule behind the admin guard: only the `admin` role may see admin routes. */
export function decideAdminAccess(session: Session): AccessDecision {
  switch (session.status) {
    case 'loading':
      return 'loading';
    case 'error':
      return 'error';
    case 'unauthenticated':
      return 'login';
    case 'authenticated':
      return session.user.role === 'admin' ? 'allow' : 'forbidden';
  }
}

const ROLE_VIEW_KEY = 'xf_role_view';

export type RoleView = 'user' | 'admin';

/** The remembered User/Admin switch choice; `admin` until the admin picks User. Safe without storage. */
export function readRoleView(): RoleView {
  try {
    return window.localStorage.getItem(ROLE_VIEW_KEY) === 'user' ? 'user' : 'admin';
  } catch {
    return 'admin';
  }
}

export function writeRoleView(view: RoleView): void {
  try {
    window.localStorage.setItem(ROLE_VIEW_KEY, view);
  } catch {
    // Storage unavailable: the switch still navigates, it is just not remembered.
  }
}

/**
 * Only the landing page `/` honours the remembered switch: an admin whose last
 * choice was Admin lands on /admin. Any other URL is a deep link and is never rewritten.
 */
export function shouldLandOnAdmin(session: Session, pathname: string): boolean {
  return (
    pathname === '/' &&
    session.status === 'authenticated' &&
    session.user.role === 'admin' &&
    readRoleView() === 'admin'
  );
}

type AdminGuardProps = {
  /** Rendered while the role is unknown: only the top bar and a spinner. */
  loading: ReactElement;
  /** Rendered when `GET /me` failed for a reason other than 401. */
  failed: ReactElement;
  /** Rendered in place of the admin shell for a signed-in non-admin. */
  forbidden: ReactElement;
  children: ReactElement;
};

/**
 * Route guard reading the session role. A non-admin never gets the admin chrome
 * or any admin data fetch: children render only for the `admin` role.
 */
export function AdminGuard({ loading, failed, forbidden, children }: AdminGuardProps) {
  const { session } = useSession();
  const { pathname, search } = useLocation();

  switch (decideAdminAccess(session)) {
    case 'loading':
      return loading;
    case 'error':
      return failed;
    case 'login':
      return <Navigate to={`/login?next=${encodeURIComponent(pathname + search)}`} replace />;
    case 'forbidden':
      return forbidden;
    case 'allow':
      return children;
  }
}
