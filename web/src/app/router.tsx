import { createBrowserRouter } from 'react-router';
import type { RouteObject } from 'react-router';
import { LoginPage } from '../pages/login/LoginPage';
import { RegisterPage } from '../pages/register/RegisterPage';
import { AdminGuard } from './guards';
import { RouteErrorPage } from './ErrorBoundary';
import { AdminGuardFailed, AdminGuardLoading, AdminShell } from './shells/AdminShell';
import { UserShell } from './shells/UserShell';
import { adminPages } from './routes/adminRoutes';
import { userPages } from './routes/userRoutes';

export const routes: RouteObject[] = [
  { path: '/login', element: <LoginPage />, errorElement: <RouteErrorPage /> },
  { path: '/register', element: <RegisterPage />, errorElement: <RouteErrorPage /> },
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
    errorElement: <RouteErrorPage />,
  },
  { path: '/', element: <UserShell />, children: userPages, errorElement: <RouteErrorPage /> },
];

export function createAppRouter() {
  return createBrowserRouter(routes);
}
