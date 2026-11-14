import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { keepMemberOutOfAdminDefaults, reviewAndRefreshPlans } from '../suites/admin';

test.use({ storageState: ADMIN_STATE });

test('admin reviews and refreshes plans', async ({ page }) => {
  await reviewAndRefreshPlans(page);
});

test('USER cannot enter the admin defaults route', async ({ page }) => {
  await keepMemberOutOfAdminDefaults(page);
});
