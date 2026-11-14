import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

export async function expectCrawlPage(page: Page): Promise<void> {
  await expect(page.getByTestId('user-crawl-page')).toBeVisible();
}

export async function expectCrawlProgressPanels(page: Page): Promise<void> {
  await expect(page.getByTestId('user-crawl-depths')).toBeVisible();
  await expect(page.getByTestId('user-crawl-lanes-card')).toBeVisible();
}

export async function expectFirstDepthColumnCount(page: Page, count: string): Promise<void> {
  await expect(page.getByTestId('user-crawl-depth-column-0').locator('span').nth(1)).toHaveText(
    count,
  );
}

/** The finished work the real crawl worker reports for this user, across every facet. */
export async function finishedCrawlWork(page: Page): Promise<number> {
  const response = await page.request.get('/me/crawl');
  expect(response.status()).toBe(200);
  const body = (await response.json()) as { facets: Array<{ done: number }> };
  return body.facets.reduce((sum, facet) => sum + facet.done, 0);
}

export async function readDepthSlider(page: Page): Promise<{ max: number; current: number }> {
  const slider = page.getByTestId('user-crawl-depth-slider');
  await expect(slider).toBeVisible();
  return {
    max: Number(await slider.getAttribute('max')),
    current: Number(await slider.inputValue()),
  };
}

export async function setDepth(page: Page, depth: number): Promise<void> {
  await page.getByTestId('user-crawl-depth-slider').fill(String(depth));
}

export async function saveDepth(page: Page): Promise<void> {
  await page.getByTestId('user-crawl-depth-save').click();
}

export async function expectCrawlToast(page: Page): Promise<void> {
  await expect(page.getByTestId('user-crawl-toast')).toBeVisible();
}

export async function expectDepth(page: Page, depth: number): Promise<void> {
  await expect(page.getByTestId('user-crawl-depth-slider')).toHaveValue(String(depth));
}
