import { expect, test } from '@playwright/test';
import { ADMIN_STATE, adminCredentials, expectShell, saveAdminState, signIn } from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

// The first registered user becomes the admin. These journeys run first and leave that admin in the database.
test.describe.serial('authentication on the real stack', () => {
  test('registers the first user, who lands signed in', async ({ page }) => {
    const watch = monitor(page);
    const { email, password } = adminCredentials();
    await page.goto('/register');
    await page.getByTestId('auth-register-email').fill(email);
    await page.getByTestId('auth-register-password').fill(password);
    const confirm = page.getByTestId('auth-register-confirm');
    if (await confirm.count()) await confirm.fill(password);
    const terms = page.getByTestId('auth-register-terms');
    if (await terms.count()) await terms.check();
    await page.getByTestId('auth-register-submit').click();
    await expectShell(page);
    await expectNoErrorPage(page, 'after registration');
    watch.expectClean('registration');
    await saveAdminState(page);
  });

  test('signs out and in again', async ({ page }) => {
    const watch = monitor(page);
    const { email, password } = adminCredentials();
    await signIn(page, email, password);
    // The admin shell has no account menu and `/` sends an admin to it; a deep link opens the member's shell.
    await page.goto('/profile');
    await page.getByTestId('shell-avatar-button').click();
    await page.getByTestId('shell-avatar-sign-out').click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByTestId('auth-login-form')).toBeVisible();
    await signIn(page, email, password);
    await expectNoErrorPage(page, 'after signing in again');
    watch.expectClean('sign-out and sign-in');
  });

  test('rejects a wrong password with a clear error and keeps the email', async ({ page }) => {
    const watch = monitor(page);
    const { email } = adminCredentials();
    await page.goto('/login');
    await page.getByTestId('auth-login-email').fill(email);
    await page.getByTestId('auth-login-password').fill('not-the-password-1');
    watch.expectFailure('POST', '/auth/login', 401);
    await page.getByTestId('auth-login-submit').click();
    await expect(page.getByTestId('auth-error-banner')).toBeVisible();
    await expect(page.getByTestId('auth-login-email')).toHaveValue(email);
    watch.expectClean('failed sign-in');
  });

  // Reuses the saved session: the sign-in form allows five attempts per quarter hour and this run needs them.
  test.describe('session expiry', () => {
    test.use({ storageState: ADMIN_STATE });

    test('sends a member whose session expired back to sign-in and returns after login', async ({
      page,
    }) => {
      const { email, password } = adminCredentials();
      await page.goto('/profile');
      await expect(page.getByTestId('profile-page')).toBeVisible();
      // Expire the session the way the server does: the session cookie goes, the CSRF cookie stays.
      for (const cookie of await page.context().cookies()) {
        if (!/csrf/i.test(cookie.name)) await page.context().clearCookies({ name: cookie.name });
      }
      // The next write the member makes is answered 401.
      await page.getByTestId('profile-account-language').selectOption('vi');
      await page.getByTestId('profile-account-save').click();
      await expect(page.getByTestId('shell-session-modal')).toBeVisible();
      await page.getByTestId('shell-session-sign-in').click();
      await expect(page).toHaveURL(/\/login\?reason=expired&return_to=%2Fprofile$/);
      await expect(page.getByTestId('auth-expired-banner')).toBeVisible();
      await page.getByTestId('auth-login-email').fill(email);
      await page.getByTestId('auth-login-password').fill(password);
      await page.getByTestId('auth-login-submit').click();
      // The sign-in screen sends an administrator to the admin overview whatever return_to says.
      await expectShell(page);
    });
  });
});
