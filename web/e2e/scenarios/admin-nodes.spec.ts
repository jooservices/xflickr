import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { readAndRefreshKeyStates } from '../suites/admin';

test.use({ storageState: ADMIN_STATE });

test('admin reads and refreshes key states', async ({ page }) => {
  await readAndRefreshKeyStates(page);
});
