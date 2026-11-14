import { test } from '@playwright/test';
import { ADMIN_STATE } from '../support/env';
import {
  changeCrawlDepthAndKeepItAfterReload,
  pauseAndResumeContact,
  showCrawlProgressAfterConnection,
  showCrawlProgressByDepthAndLanes,
} from '../suites/crawl';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('crawl on the real stack', () => {
  test('shows progress by depth and the queue lanes', async ({ page }) => {
    await showCrawlProgressByDepthAndLanes(page);
  });

  test('shows crawl progress after account connection', async ({ page }) => {
    await showCrawlProgressAfterConnection(page);
  });

  test('changes the crawl depth and keeps it after a reload', async ({ page }) => {
    await changeCrawlDepthAndKeepItAfterReload(page);
  });

  test('pauses and resumes a contact', async ({ page }) => {
    await pauseAndResumeContact(page);
  });
});
