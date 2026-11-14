import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import { browseObservedPeopleAndPhotos } from '../suites/catalog';

test.use({ storageState: ADMIN_STATE });

test('browses people and photos observed by the connected account crawl', async ({ page }) => {
  await browseObservedPeopleAndPhotos(page);
});
