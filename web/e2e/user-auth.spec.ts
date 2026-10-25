import { expect, test } from '@playwright/test';
import { mockApi } from './support';

test.describe('user authentication', () => {
  test('signs in and signs out', async ({ page }) => {
    await mockApi(page, 'anonymous');
    await page.route('**/auth/config', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ registration_open: true, passkey_enabled: false }),
      }),
    );
    await page.route('**/auth/login', async (route) => {
      expect(route.request().postDataJSON()).toMatchObject({
        email: 'anna@example.test',
        password: 'password123',
      });
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 2,
          email: 'anna@example.test',
          role: 'user',
          locale: 'en',
          created_at: '2026-10-01T00:00:00Z',
        }),
      });
    });
    await page.route('**/auth/logout', (route) => route.fulfill({ status: 204 }));
    await page.goto('/login');
    await page.getByTestId('auth-login-email').fill('anna@example.test');
    await page.getByTestId('auth-login-password').fill('password123');
    await page.getByTestId('auth-login-submit').click();
    await expect(page).toHaveURL(/\/$/);
    await page.getByTestId('shell-avatar-button').click();
    await page.getByTestId('shell-avatar-sign-out').click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByTestId('auth-login-form')).toBeVisible();
  });

  test('returns to the original page after a 401 expires the session', async ({ page }) => {
    let authenticated = true;
    await mockApi(page, 'user');
    await page.route('**/auth/config', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ registration_open: true, passkey_enabled: false }),
      }),
    );
    await page.route('**/me', async (route) => {
      if (route.request().method() === 'PATCH') {
        authenticated = false;
        await route.fulfill({
          status: 401,
          contentType: 'application/json',
          body: JSON.stringify({ code: 'unauthenticated' }),
        });
      } else if (!authenticated)
        await route.fulfill({
          status: 401,
          contentType: 'application/json',
          body: JSON.stringify({ code: 'unauthenticated' }),
        });
      else await route.fallback();
    });
    await page.route('**/auth/login', (route) => {
      authenticated = true;
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ id: 1, email: 'user@example.test', role: 'user', locale: 'en' }),
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
    await page.route('**/auth/config', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ registration_open: true, passkey_enabled: false }),
      }),
    );
    await page.route('**/auth/login', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ id: 2, email: 'anna@example.test', role: 'user', locale: 'en' }),
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
