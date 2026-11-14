import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { addValidKey, rejectDeadKeys, rejectInvalidKey, showEmptyKeys } from '../suites/keys';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('API keys and quota on the real stack', () => {
  test.beforeEach(async ({ request }) => {
    await rejectDeadKeys(request);
  });

  test('shows the empty state before any key exists', async ({ page }) => {
    await showEmptyKeys(page);
  });

  test('rejects an invalid key with a clear error and keeps what was typed', async ({ page }) => {
    await rejectInvalidKey(page);
  });

  test('adds a valid key and shows its usage', async ({ page }) => {
    await addValidKey(page);
  });
});
