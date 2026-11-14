import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { selectFavoritesWithinTheLimit } from '../suites/library';

test.use({ storageState: ADMIN_STATE });

test('selects visible favorites without losing filtered selection', async ({ page }) => {
  await selectFavoritesWithinTheLimit(page);
});
