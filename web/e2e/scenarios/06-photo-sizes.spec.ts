import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import {
  confirmOwnerRestrictedSizesWithAccountToken,
  confirmProvisionalSizesByDownloadRequest,
} from '../suites/catalog';

test.use({ storageState: ADMIN_STATE });

test('confirms provisional photo sizes through an explicit download request', async ({ page }) => {
  await confirmProvisionalSizesByDownloadRequest(page);
});

test('confirms the owner restricted photo sizes with the connected account token', async ({
  page,
  request,
}) => {
  await confirmOwnerRestrictedSizesWithAccountToken(page, request);
});
