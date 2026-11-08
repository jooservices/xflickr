import { expect, test } from '@playwright/test';
import { ADMIN_STATE, apiWrite, setFlickrScenario } from './support/auth';
import { observeCrawledPhoto } from './support/catalog';
import { expectNoErrorPage, monitor } from './support/monitor';
import { worktreeSQL } from './support/worktree';

test.use({ storageState: ADMIN_STATE });

test('confirms provisional photo sizes through an explicit download request', async ({ page }) => {
  const watch = monitor(page);
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-page')).toBeVisible();
  const photoID = 52000000001;
  await expect.poll(() => observeCrawledPhoto(photoID), { timeout: 15_000 }).toBe(true);
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
  const downloadID = (created.body as { id: number }).id;
  expect(Number.isSafeInteger(downloadID)).toBe(true);
  await expect
    .poll(
      () =>
        worktreeSQL(
          `SELECT state IN ('pending','fetching','ready') FROM download_items WHERE download_id=${downloadID} AND photo_id=${photoID}`,
        ),
      { timeout: 15_000 },
    )
    .toBe('t');
  expect(
    worktreeSQL(
      `SELECT count(*) FROM river_job WHERE args->>'kind'='media.fetch' AND args->'args'->>'download_id'='${downloadID}'`,
    ),
  ).toBe('1');
  // A second explicit request reuses confirmed sizes through the direct path.
  const repeated = await apiWrite(page, 'POST', '/downloads', {
    kind: 'tray',
    photo_ids: [photoID],
  });
  expect(repeated.status).toBe(201);
  const repeatedID = (repeated.body as { id: number }).id;
  expect(Number.isSafeInteger(repeatedID)).toBe(true);
  expect(
    worktreeSQL(
      `SELECT state IN ('pending','fetching','ready') FROM download_items WHERE download_id=${repeatedID} AND photo_id=${photoID}`,
    ),
  ).toBe('t');
  await expectNoErrorPage(page, '/crawl after a direct download request');
  watch.expectClean('on-demand sizes');
});

test('confirms the owner restricted photo sizes with the connected account token', async ({
  page,
  request,
}) => {
  const watch = monitor(page);
  await setFlickrScenario(request, {});
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-page')).toBeVisible();
  const photoID = 52000000004;
  await expect.poll(() => observeCrawledPhoto(photoID), { timeout: 30_000 }).toBe(true);
  expect(
    worktreeSQL(
      `UPDATE photos SET server='65535', sizes_status='provisional', sizes_checked_at=NULL WHERE id=${photoID} RETURNING server`,
    ),
  ).toBe('65535');
  const created = await apiWrite(page, 'POST', '/downloads', {
    kind: 'tray',
    photo_ids: [photoID],
  });
  expect(created.status).toBe(201);
  await expect
    .poll(
      async () => {
        const response = await page.request.get(`/photos/${photoID}`);
        expect(response.status()).toBe(200);
        return ((await response.json()) as { sizes_status: string }).sizes_status;
      },
      { timeout: 15_000 },
    )
    .toBe('confirmed');
  await expectNoErrorPage(page, '/crawl after restricted sizes confirmation');
  watch.expectClean('restricted owner sizes');
});
