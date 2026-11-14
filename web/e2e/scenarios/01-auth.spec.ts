import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import {
  recoverExpiredSession,
  registerFirstUser,
  rejectWrongPassword,
  signOutAndInAgain,
} from '../suites/auth';

// The first registered user becomes the admin. These journeys run first and leave that admin in the database.
test.describe.serial('authentication on the real stack', () => {
  test('registers the first user, who lands signed in', async ({ page }) => {
    await registerFirstUser(page);
  });

  test('signs out and in again', async ({ page }) => {
    await signOutAndInAgain(page);
  });

  test('rejects a wrong password with a clear error and keeps the email', async ({ page }) => {
    await rejectWrongPassword(page);
  });

  // Reuses the saved session: the sign-in form allows five attempts per quarter hour and this run needs them.
  test.describe('session expiry', () => {
    test.use({ storageState: ADMIN_STATE });

    test('sends a member whose session expired back to sign-in and returns after login', async ({
      page,
    }) => {
      await recoverExpiredSession(page);
    });
  });
});
