import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { confirmProvisionalSizesByDownloadRequest } from '../suites/catalog';

test.use({ storageState: ADMIN_STATE });

test('confirms provisional photo sizes through an explicit download request', async ({ page }) => {
  await confirmProvisionalSizesByDownloadRequest(page);
});
