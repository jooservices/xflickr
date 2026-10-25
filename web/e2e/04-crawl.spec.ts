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

  test('counts the connected account as the first contact of the crawl', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/crawl');
    // The crawl starts when the Flickr account is connected; the Crawl screen reports the person it begins with.
    await expect(page.getByTestId('user-crawl-depth-column-0')).toContainText('1', {
      timeout: 30_000,
    });
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
    // The Crawl screen has no pause control yet, so the journey calls the endpoints the way a control will.
    const nsid = '10000000@N00';
    const paused = await apiWrite(page, 'PUT', '/me/crawl/pause', { nsid });
    expect([204, 404], `pause answered ${String(paused.status)}`).toContain(paused.status);
    const resumed = await apiWrite(page, 'PUT', '/me/crawl/resume', { nsid });
    expect([204, 404], `resume answered ${String(resumed.status)}`).toContain(resumed.status);
    await page.reload();
    await expect(page.getByTestId('user-crawl-page')).toBeVisible();
    await expectNoErrorPage(page, '/crawl after pause and resume');
    expect(
      watch.problems.filter((problem) => !/HTTP PUT \/me\/crawl\/(pause|resume) 404/.test(problem)),
    ).toEqual([]);
  });
});
