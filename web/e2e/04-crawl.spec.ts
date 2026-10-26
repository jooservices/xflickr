import { expect, test } from '@playwright/test';
import { ADMIN_STATE, apiWrite } from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('crawl on the real stack', () => {
  test('shows progress by depth and the queue lanes', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/crawl');
    await expect(page.getByTestId('user-crawl-page')).toBeVisible();
    await expect(page.getByTestId('user-crawl-depths')).toBeVisible();
    await expect(page.getByTestId('user-crawl-lanes-card')).toBeVisible();
    await expectNoErrorPage(page, '/crawl');
    watch.expectClean('/crawl');
  });

  test('shows crawl progress after account connection', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/crawl');
    await expect(page.getByTestId('user-crawl-depth-column-0').locator('span').nth(1)).toHaveText(
      '1',
    );
    // Account connection starts the crawl. Confirm the real worker completed work for this user.
    await expect
      .poll(
        async () => {
          const response = await page.request.get('/me/crawl');
          expect(response.status()).toBe(200);
          const body = (await response.json()) as {
            facets: Array<{ done: number }>;
          };
          return body.facets.reduce((sum, facet) => sum + facet.done, 0);
        },
        { timeout: 15_000 },
      )
      .toBeGreaterThan(0);
    watch.expectClean('/crawl progress');
  });

  test('changes the crawl depth and keeps it after a reload', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/crawl');
    const slider = page.getByTestId('user-crawl-depth-slider');
    await expect(slider).toBeVisible();
    const max = Number(await slider.getAttribute('max'));
    const current = Number(await slider.inputValue());
    const target = current > 1 ? current - 1 : Math.min(2, max);
    await slider.fill(String(target));
    await page.getByTestId('user-crawl-depth-save').click();
    await expect(page.getByTestId('user-crawl-toast')).toBeVisible();
    await page.reload();
    await expect(page.getByTestId('user-crawl-depth-slider')).toHaveValue(String(target));
    watch.expectClean('/crawl depth change');
  });

  test('pauses and resumes a contact', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/crawl');
    await expect(page.getByTestId('user-crawl-page')).toBeVisible();
    const nsid = '10000000@N00';
    const paused = await apiWrite(page, 'PUT', '/me/crawl/pause', { nsid });
    expect(paused.status, 'pause endpoint must succeed').toBe(204);
    const resumed = await apiWrite(page, 'PUT', '/me/crawl/resume', { nsid });
    expect(resumed.status, 'resume endpoint must succeed').toBe(204);

    await page.reload();
    await expect(page.getByTestId('user-crawl-page')).toBeVisible();
    await expectNoErrorPage(page, '/crawl after pause and resume');
    watch.expectClean('/crawl pause and resume');
  });
});
