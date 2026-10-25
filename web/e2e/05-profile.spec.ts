import { expect, test } from '@playwright/test';
import { ADMIN_STATE, adminCredentials } from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

test.use({ storageState: ADMIN_STATE });

test('profile page shows the signed-in account', async ({ page }) => {
  const watch = monitor(page);
  const { email } = adminCredentials();
  await page.goto('/profile');
  await expect(page.getByTestId('profile-page')).toBeVisible();
  await expect(page.getByTestId('profile-account-email')).toContainText(email);
  await expect(page.getByTestId('profile-password-card')).toBeVisible();
  await expectNoErrorPage(page, '/profile');
  watch.expectClean('/profile');
});
