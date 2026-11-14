import type { Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { SEEDED_NSID } from '../support/env';
import { apiWrite } from '../steps/api';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import {
  expectCrawlPage,
  expectCrawlProgressPanels,
  expectCrawlToast,
  expectDepth,
  expectFirstDepthColumnCount,
  finishedCrawlWork,
  readDepthSlider,
  saveDepth,
  setDepth,
} from '../steps/crawl';
import { openPage, reloadPage } from '../steps/session';

export async function showCrawlProgressByDepthAndLanes(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/crawl');
  await expectCrawlPage(page);
  await expectCrawlProgressPanels(page);
  await expectNoErrorPage(page, '/crawl');
  watch.expectClean('/crawl');
}

/** Account connection starts the crawl; the real worker must complete work for this user. */
export async function showCrawlProgressAfterConnection(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/crawl');
  await expectFirstDepthColumnCount(page, '1');
  await expect.poll(() => finishedCrawlWork(page), { timeout: 15_000 }).toBeGreaterThan(0);
  watch.expectClean('/crawl progress');
}

export async function changeCrawlDepthAndKeepItAfterReload(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/crawl');
  const { max, current } = await readDepthSlider(page);
  const target = current > 1 ? current - 1 : Math.min(2, max);
  await setDepth(page, target);
  await saveDepth(page);
  await expectCrawlToast(page);
  await reloadPage(page);
  await expectDepth(page, target);
  watch.expectClean('/crawl depth change');
}

export async function pauseAndResumeContact(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/crawl');
  await expectCrawlPage(page);
  const paused = await apiWrite(page, 'PUT', '/me/crawl/pause', { nsid: SEEDED_NSID });
  expect(paused.status, 'pause endpoint must succeed').toBe(204);
  const resumed = await apiWrite(page, 'PUT', '/me/crawl/resume', { nsid: SEEDED_NSID });
  expect(resumed.status, 'resume endpoint must succeed').toBe(204);
  await reloadPage(page);
  await expectCrawlPage(page);
  await expectNoErrorPage(page, '/crawl after pause and resume');
  watch.expectClean('/crawl pause and resume');
}
