import { createBrowserRouter, useNavigate } from 'react-router';
import type { RouteObject } from 'react-router';
import { PlaceholderPage } from '../pages';
import { NotFoundState } from '../shared';
import { AdminGuard } from './guards';
import { AdminGuardFailed, AdminGuardLoading, AdminShell } from './shells/AdminShell';
import { UserShell } from './shells/UserShell';
import { ADMIN_NAV, USER_NAV } from './shells/nav';
import { LoginPage } from '../pages/login/LoginPage';
import { RegisterPage } from '../pages/register/RegisterPage';
import { ProfilePage } from '../pages/profile/ProfilePage';
import { SetupPage } from '../pages/setup/SetupPage';

const userPages: RouteObject[] = [
  {
    index: true,
    element: (
      <PlaceholderPage
        scope="shell"
        titleKey="shell.nav.home"
        emptyIcon="image"
        emptyTitleKey="shell.home.empty.title"
        emptyTextKey="shell.home.empty.text"
      />
    ),
  },
  ...USER_NAV.filter((item) => item.path !== '/').map((item): RouteObject => ({
    path: item.path.slice(1),
    element:
      item.path === '/profile' ? (
        <ProfilePage />
      ) : (
        <PlaceholderPage scope="shell" titleKey={item.labelKey} />
      ),
  })),
  {
    path: 'contacts/:id',
    element: <PlaceholderPage scope="shell" titleKey="shell.page.contact" />,
  },
  { path: 'albums/:id', element: <PlaceholderPage scope="shell" titleKey="shell.page.album" /> },
  { path: 'accounts', element: <PlaceholderPage scope="shell" titleKey="shell.page.accounts" /> },
  { path: 'sessions', element: <PlaceholderPage scope="shell" titleKey="shell.page.sessions" /> },
  {
    path: 'onboarding',
    element: <SetupPage />,
  },
  { path: '*', element: <UserNotFound /> },
];

const adminPages: RouteObject[] = [
  ...ADMIN_NAV.map((item): RouteObject =>
    item.path === '/admin'
      ? { index: true, element: <PlaceholderPage scope="admin" titleKey={item.labelKey} /> }
      : {
          path: item.path.slice('/admin/'.length),
          element: <PlaceholderPage scope="admin" titleKey={item.labelKey} />,
        },
  ),
  { path: 'nodes/:id', element: <PlaceholderPage scope="admin" titleKey="admin.nav.nodes" /> },
  { path: '*', element: <UserNotFound /> },
];

function UserNotFound() {
  const navigate = useNavigate();

  return (
    <NotFoundState
      testId="shell-notfound"
      onHome={() => {
        void navigate('/');
      }}
      onBack={() => {
        void navigate(-1);
      }}
    />
  );
}

export const routes: RouteObject[] = [
  { path: '/login', element: <LoginPage /> },
  { path: '/register', element: <RegisterPage /> },
  {
    path: '/admin',
    element: (
      <AdminGuard
        loading={<AdminGuardLoading />}
        failed={<AdminGuardFailed />}
        forbidden={<UserShell forbidden />}
      >
        <AdminShell />
      </AdminGuard>
    ),
    children: adminPages,
  },
  { path: '/', element: <UserShell />, children: userPages },
];

export function createAppRouter() {
  return createBrowserRouter(routes);
}
