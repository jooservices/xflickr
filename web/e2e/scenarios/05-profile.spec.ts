import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import {
  changePasswordAndKeepSession,
  revokeSessions,
  showSignedInAccount,
  updateAndRestoreSharing,
} from '../suites/profile';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('profile identity actions on the real stack', () => {
  test('profile page shows the signed-in account', async ({ page }) => {
    await showSignedInAccount(page);
  });

  test('revokes one session and signs out every other session', async ({ browser, page }) => {
    await revokeSessions(browser, page);
  });

  test('changes the password and keeps the current session active', async ({ page }) => {
    await changePasswordAndKeepSession(page);
  });

  test('updates and restores the sharing choice through the REST resource', async ({ page }) => {
    await updateAndRestoreSharing(page);
  });
});
