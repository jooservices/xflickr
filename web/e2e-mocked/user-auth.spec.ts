import { expect } from '@playwright/test';

import { signInAs as signIn } from '../e2e/suites/auth';
import { runScenario } from '../e2e/support/scenario';
import { mockApi, test } from './support';

test.describe('user authentication', () => {
  test('signs in and signs out', async ({ page }) => {
    await mockApi(page, 'anonymous');
    await page.route('**/api/v1/authentication-options', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          code: 200,
          message: 'OK',
          data: {
            registration_open: true,
            passkey_enabled: false,
            password_min: 8,
            password_max: 128,
            links: { self: { href: '/api/v1/authentication-options', method: 'GET' } },
          },
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      }),
    );
    await page.route('**/api/v1/sessions', async (route) => {
      expect(route.request().postDataJSON()).toMatchObject({
        email: 'anna@example.test',
        password: 'password123',
      });
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          code: 201,
          message: 'Created',
          data: {
            id: 'session-1',
            current: true,
            user_agent_label: 'Chromium on Linux',
            ip_masked: '127.0.0.x',
            last_seen_at: '2026-10-01T00:00:00Z',
            created_at: '2026-10-01T00:00:00Z',
            expires_at: '2026-11-01T00:00:00Z',
            links: {
              self: { href: '/api/v1/sessions/current', method: 'GET' },
              user: { href: '/api/v1/users/2', method: 'GET' },
            },
            user: {
              id: 2,
              email: 'anna@example.test',
              role: 'user',
              locale: 'en',
              created_at: '2026-10-01T00:00:00Z',
              links: { self: { href: '/api/v1/users/2', method: 'GET' } },
            },
          },
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      });
    });
    await page.route('**/api/v1/sessions/current', (route) =>
      route.request().method() === 'DELETE' ? route.fulfill({ status: 204 }) : route.fallback(),
    );
    await runScenario(page, [(p) => signIn(p, 'anna@example.test', 'password123')]);
    await expect(page).toHaveURL(/\/$/);
    await page.getByTestId('shell-avatar-button').click();
    await page.getByTestId('shell-avatar-sign-out').click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByTestId('auth-login-form')).toBeVisible();
  });

  test('returns to the original page after a 401 expires the session', async ({ page }) => {
    let authenticated = true;
    await mockApi(page, 'user');
    await page.route('**/api/v1/authentication-options', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          code: 200,
          message: 'OK',
          data: {
            registration_open: true,
            passkey_enabled: false,
            password_min: 8,
            password_max: 128,
            links: { self: { href: '/api/v1/authentication-options', method: 'GET' } },
          },
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      }),
    );
    await page.route('**/api/v1/me', async (route) => {
      if (route.request().method() === 'PATCH') {
        authenticated = false;
        await route.fulfill({
          status: 401,
          contentType: 'application/json',
          body: JSON.stringify({
            type: '/problems/unauthenticated',
            title: 'Unauthenticated',
            status: 401,
            detail: 'Sign in required',
            instance: '/api/v1/me',
            trace_id: 'test-trace',
            errors: null,
          }),
        });
      } else if (!authenticated)
        await route.fulfill({
          status: 401,
          contentType: 'application/json',
          body: JSON.stringify({
            type: '/problems/unauthenticated',
            title: 'Unauthenticated',
            status: 401,
            detail: 'Sign in required',
            instance: '/api/v1/me',
            trace_id: 'test-trace',
            errors: null,
          }),
        });
      else await route.fallback();
    });
    await page.route('**/api/v1/sessions', (route) => {
      authenticated = true;
      return route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          code: 201,
          message: 'Created',
          data: {
            id: 'session-2',
            current: true,
            user_agent_label: 'Chromium on Linux',
            ip_masked: '127.0.0.x',
            last_seen_at: '2026-01-01T00:00:00Z',
            created_at: '2026-01-01T00:00:00Z',
            expires_at: '2026-02-01T00:00:00Z',
            links: { self: { href: '/api/v1/sessions/current', method: 'GET' } },
            user: {
              id: 1,
              email: 'user@example.test',
              role: 'user',
              locale: 'en',
              created_at: '2026-01-01T00:00:00Z',
              links: { self: { href: '/api/v1/users/1', method: 'GET' } },
            },
          },
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      });
    });
    await page.goto('/profile');
    await page.getByTestId('profile-account-language').selectOption('vi');
    await page.getByTestId('profile-account-save').click();
    await expect(page.getByTestId('shell-session-modal')).toBeVisible();
    await page.getByTestId('shell-session-sign-in').click();
    await expect(page).toHaveURL(/\/login\?reason=expired&return_to=%2Fprofile$/);
    await page.getByTestId('auth-login-email').fill('user@example.test');
    await page.getByTestId('auth-login-password').fill('password123');
    await page.getByTestId('auth-login-submit').click();
    await expect(page).toHaveURL(/\/profile$/);
    await expect(page.getByTestId('profile-page')).toBeVisible();
  });

  test('drops a hostile return_to before redirecting after sign-in', async ({ page }) => {
    await mockApi(page, 'anonymous');
    await page.route('**/api/v1/authentication-options', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          code: 200,
          message: 'OK',
          data: {
            registration_open: true,
            passkey_enabled: false,
            password_min: 8,
            password_max: 128,
            links: { self: { href: '/api/v1/authentication-options', method: 'GET' } },
          },
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      }),
    );
    await page.route('**/api/v1/sessions', (route) =>
      route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          code: 201,
          message: 'Created',
          data: {
            id: 'session-1',
            current: true,
            user_agent_label: 'Chromium on Linux',
            ip_masked: '127.0.0.x',
            last_seen_at: '2026-01-01T00:00:00Z',
            created_at: '2026-01-01T00:00:00Z',
            expires_at: '2026-02-01T00:00:00Z',
            links: { self: { href: '/api/v1/sessions/current', method: 'GET' } },
            user: {
              id: 2,
              email: 'anna@example.test',
              role: 'user',
              locale: 'en',
              created_at: '2026-01-01T00:00:00Z',
              links: { self: { href: '/api/v1/users/2', method: 'GET' } },
            },
          },
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      }),
    );
    await page.goto('/login?reason=expired&return_to=%2F%2Fevil.example');
    await page.getByTestId('auth-login-email').fill('anna@example.test');
    await page.getByTestId('auth-login-password').fill('password123');
    await page.getByTestId('auth-login-submit').click();
    await expect(page).toHaveURL(/127\.0\.0\.1:\d+\/$/);
    await expect(page.getByTestId('shell-nav')).toBeVisible();
  });
});
