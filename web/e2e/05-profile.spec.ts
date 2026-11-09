import { expect, test } from '@playwright/test';
import type { Locator, Page } from '@playwright/test';
import {
  ADMIN_STATE,
  LAST_SESSION_STATE,
  OTHER_SESSION_STATE,
  adminCredentials,
  apiRead,
  apiWrite,
} from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('profile identity actions on the real stack', () => {
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

  test('revokes one session and signs out every other session', async ({ browser, page }) => {
    const watch = monitor(page);
    await page.goto('/profile');
    await expect(page.getByTestId(/^profile-session-row-/).first()).toBeVisible();

    const origin = new URL(page.url()).origin;
    const secondaryContexts = [];
    const secondaryPages: Page[] = [];
    try {
      for (const storageState of [OTHER_SESSION_STATE, LAST_SESSION_STATE]) {
        const context = await browser.newContext({ baseURL: origin, storageState });
        secondaryContexts.push(context);
        const secondaryPage = await context.newPage();
        secondaryPages.push(secondaryPage);
        await secondaryPage.goto('/profile');
        await expect(secondaryPage.getByTestId('profile-page')).toBeVisible();
      }

      const rows = page.locator('li[data-testid^="profile-session-row-"]');
      await expect(rows).toHaveCount(3);

      let revokedRow: Locator | undefined;
      for (let index = 0; index < (await rows.count()); index += 1) {
        const row = rows.nth(index);
        if ((await row.getByTestId('profile-session-current').count()) === 0) {
          revokedRow = row;
          break;
        }
      }
      if (!revokedRow) throw new Error('A non-current session should be listed');
      const rowTestId = await revokedRow.getAttribute('data-testid');
      if (!rowTestId) throw new Error('A session row must have a test id');
      expect(rowTestId).toMatch(/^profile-session-row-/);
      const sessionID = rowTestId.replace('profile-session-row-', '');
      await page.getByTestId(`profile-session-revoke-${sessionID}`).click();
      await page.getByTestId('profile-confirm-ok').click();
      await expect(page.getByTestId(rowTestId)).toHaveCount(0);

      await page.getByTestId('profile-sessions-revoke-others').click();
      await page.getByTestId('profile-confirm-ok').click();
      await expect(rows).toHaveCount(1);
      await expect(rows.first().getByTestId('profile-session-current')).toBeVisible();

      for (const secondaryPage of secondaryPages) {
        const response = await apiRead(secondaryPage, '/api/v1/me');
        expect(response.status).toBe(401);
      }

      await expectNoErrorPage(page, '/profile after revoking sessions');
      watch.expectClean('/profile session revocation');
    } finally {
      await Promise.all(secondaryContexts.map((context) => context.close()));
    }
  });

  test('changes the password and keeps the current session active', async ({ page }) => {
    const watch = monitor(page);
    const { password } = adminCredentials();
    await page.goto('/profile');
    await expect(page.getByTestId('profile-page')).toBeVisible();

    const temporaryPassword = `E2e-${Date.now()}-Password!`;
    await changePassword(page, password, temporaryPassword);
    await changePassword(page, temporaryPassword, password);

    expect((await apiRead(page, '/api/v1/me')).status).toBe(200);
    await expectNoErrorPage(page, '/profile after changing password');
    watch.expectClean('/profile password update');
  });

  test('updates and restores the sharing choice through the REST resource', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/profile');
    const origin = new URL(page.url()).origin;
    const currentResponse = await apiRead(page, `${origin}/api/v1/me/sharing`);
    expect(currentResponse.status).toBe(200);
    const currentBody = currentResponse.body as {
      data?: { enabled?: unknown };
    };
    const originalEnabled = currentBody.data?.enabled;
    if (typeof originalEnabled !== 'boolean') {
      throw new Error('Sharing response must include the enabled value');
    }

    try {
      const updated = await apiWrite(page, 'PUT', '/api/v1/me/sharing', {
        enabled: !originalEnabled,
      });
      expect(updated.status).toBe(200);
      expect((updated.body as { data?: { enabled?: unknown } }).data?.enabled).toBe(
        !originalEnabled,
      );
    } finally {
      const restored = await apiWrite(page, 'PUT', '/api/v1/me/sharing', {
        enabled: originalEnabled,
      });
      expect(restored.status).toBe(200);
      expect((restored.body as { data?: { enabled?: unknown } }).data?.enabled).toBe(
        originalEnabled,
      );
    }

    await expectNoErrorPage(page, '/profile sharing update');
    watch.expectClean('/profile sharing update');
  });
});

async function changePassword(page: Page, current: string, next: string) {
  await page.getByTestId('profile-password-current').fill(current);
  await page.getByTestId('profile-password-new').fill(next);
  await page.getByTestId('profile-password-confirm').fill(next);
  const response = page.waitForResponse(
    (candidate) =>
      candidate.url().endsWith('/api/v1/me/password') && candidate.request().method() === 'PUT',
  );
  await page.getByTestId('profile-password-submit').click();
  expect((await response).status()).toBe(204);
  const profileStatus = await page.evaluate(async () => {
    const profile = await fetch('/api/v1/me', { credentials: 'same-origin' });
    return profile.status;
  });
  expect(profileStatus).toBe(200);
  await expect(page.getByTestId('profile-password-current')).toHaveValue('');
  await expect(page.getByTestId('profile-password-new')).toHaveValue('');
  await expect(page.getByTestId('profile-password-confirm')).toHaveValue('');
}
