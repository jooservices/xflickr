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

let bootHandled = false;

/** Marks the first navigation of this page load as handled (call it from an effect). */
export function markBootHandled(): void {
  bootHandled = true;
}

/** Test helper: a new page load. */
export function resetBootHandled(): void {
  bootHandled = false;
}

/**
 * An admin who lands on a user route when the app boots goes to /admin once;
 * after that the User/Admin switch in the top bar can open the user view.
 */
export function shouldRedirectAdminOnBoot(session: Session): boolean {
  return !bootHandled && session.status === 'authenticated' && session.user.role === 'admin';
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
