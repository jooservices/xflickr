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
});
