import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { beforeEach, describe, expect, it } from 'vitest';
import { SessionContext } from '../entities';
import type { Session } from '../entities';
import {
  AdminGuard,
  decideAdminAccess,
  resetBootHandled,
  shouldRedirectAdminOnBoot,
  markBootHandled,
} from './guards';

const admin: Session = {
  status: 'authenticated',
  user: { id: 1, email: 'a@x.test', role: 'admin', locale: 'en' },
};
const user: Session = {
  status: 'authenticated',
  user: { id: 2, email: 'u@x.test', role: 'user', locale: 'en' },
};

describe('decideAdminAccess', () => {
  it.each([
    [{ status: 'loading' }, 'loading'],
    [{ status: 'error' }, 'error'],
    [{ status: 'unauthenticated' }, 'login'],
    [admin, 'allow'],
    [user, 'forbidden'],
  ] as [Session, string][])('maps %j to %s', (session, expected) => {
    expect(decideAdminAccess(session)).toBe(expected);
  });
});

function LoginProbe() {
  const { pathname, search } = useLocation();
  return <p data-testid="login">{pathname + search}</p>;
}

function renderGuard(session: Session) {
  return render(
    <SessionContext.Provider value={{ session, retry: () => undefined }}>
      <MemoryRouter initialEntries={['/admin/users?page=2']}>
        <Routes>
          <Route path="/login" element={<LoginProbe />} />
          <Route
            path="/admin/*"
            element={
              <AdminGuard
                loading={<p>loading</p>}
                failed={<p>failed</p>}
                forbidden={<p>forbidden</p>}
              >
                <p>admin area</p>
              </AdminGuard>
            }
          />
        </Routes>
      </MemoryRouter>
    </SessionContext.Provider>,
  );
}

describe('AdminGuard', () => {
  it('renders the admin area only for the admin role', () => {
    renderGuard(admin);

    expect(screen.getByText('admin area')).toBeInTheDocument();
  });

  it('blocks a non-admin without rendering the children', () => {
    renderGuard(user);

    expect(screen.getByText('forbidden')).toBeInTheDocument();
    expect(screen.queryByText('admin area')).toBeNull();
  });

  it('shows only the loading element while the role is unknown', () => {
    renderGuard({ status: 'loading' });

    expect(screen.getByText('loading')).toBeInTheDocument();
    expect(screen.queryByText('admin area')).toBeNull();
  });

  it('shows the failure element when /me failed', () => {
    renderGuard({ status: 'error' });

    expect(screen.getByText('failed')).toBeInTheDocument();
  });

  it('sends a signed-out visitor to /login with the path to return to', () => {
    renderGuard({ status: 'unauthenticated' });

    expect(screen.getByTestId('login')).toHaveTextContent(
      '/login?next=%2Fadmin%2Fusers%3Fpage%3D2',
    );
  });
});

describe('shouldRedirectAdminOnBoot', () => {
  beforeEach(resetBootHandled);

  it('redirects an admin once per page load, never a user or an unknown session', () => {
    expect(shouldRedirectAdminOnBoot(admin)).toBe(true);
    expect(shouldRedirectAdminOnBoot(user)).toBe(false);
    expect(shouldRedirectAdminOnBoot({ status: 'loading' })).toBe(false);
    markBootHandled();
    expect(shouldRedirectAdminOnBoot(admin)).toBe(false);
  });
});
