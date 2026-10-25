import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SessionContext } from '../entities';
import type { Session } from '../entities';
import {
  AdminGuard,
  decideAdminAccess,
  readRoleView,
  shouldLandOnAdmin,
  writeRoleView,
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

describe('role view and landing', () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it('lands an admin on /admin only from / and only while the choice is Admin', () => {
    expect(shouldLandOnAdmin(admin, '/')).toBe(true);
    expect(shouldLandOnAdmin(admin, '/keys')).toBe(false);
    expect(shouldLandOnAdmin(admin, '/library')).toBe(false);
    writeRoleView('user');
    expect(shouldLandOnAdmin(admin, '/')).toBe(false);
    writeRoleView('admin');
    expect(shouldLandOnAdmin(admin, '/')).toBe(true);
  });

  it('never lands a user or an unknown session on /admin', () => {
    expect(shouldLandOnAdmin(user, '/')).toBe(false);
    expect(shouldLandOnAdmin({ status: 'loading' }, '/')).toBe(false);
  });

  it('is safe when storage throws', () => {
    const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });

    expect(readRoleView()).toBe('admin');
    expect(() => {
      writeRoleView('user');
    }).not.toThrow();
    get.mockRestore();
    set.mockRestore();
  });
});
