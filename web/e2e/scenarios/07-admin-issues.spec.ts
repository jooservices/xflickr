import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { showSafeIncidentFields } from '../suites/admin';

test.use({ storageState: ADMIN_STATE });

test('an admin sees only safe fields for an open incident', async ({ page }) => {
  await showSafeIncidentFields(page);
});
