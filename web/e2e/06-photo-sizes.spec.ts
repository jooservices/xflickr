import { expect, test } from '@playwright/test';
import { ADMIN_STATE, apiWrite } from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

test.use({ storageState: ADMIN_STATE });

test('confirms provisional photo sizes through an explicit download request', async ({ page }) => {
  const watch = monitor(page);
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-page')).toBeVisible();
  const photoID = 52000000001;
  const photo = async () => {
    const response = await page.request.get(`/photos/${photoID}`);
    expect(response.status()).toBe(200);
    return (await response.json()) as { sizes_status: string };
  };
  // The preceding account journey crawls this listing without starring or
  // selecting its owner. Ordinary catalog discovery must leave it provisional.
  await expect.poll(async () => (await photo()).sizes_status).toBe('provisional');
  const created = await apiWrite(page, 'POST', '/downloads', {
    kind: 'tray',
    photo_ids: [photoID],
  });
  expect(created.status).toBe(201);
  expect(created.body).toMatchObject({ item_count: 1, skipped: 0 });
  await expect
    .poll(async () => (await photo()).sizes_status, { timeout: 15_000 })
    .toBe('confirmed');
  await expectNoErrorPage(page, '/crawl after a direct download request');
  watch.expectClean('on-demand sizes');
});
