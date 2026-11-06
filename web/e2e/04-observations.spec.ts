import { expect, test } from '@playwright/test';
import { ADMIN_STATE } from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

test.use({ storageState: ADMIN_STATE });

test('browses people and photos observed by the connected account crawl', async ({ page }) => {
  const watch = monitor(page);
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-page')).toBeVisible();
  // The catalog screens are placeholders on this base. Exercise their real
  // authenticated read boundary in the browser, with no SQL observation seed.
  const nsid = '10000000@N00';
  await expect
    .poll(
      async () => {
        const response = await page.request.get(`/people/${encodeURIComponent(nsid)}/photos`);
        expect(response.status()).toBe(200);
        const body = (await response.json()) as { items: Array<{ id: number }> };
        return body.items.length;
      },
      { timeout: 30_000 },
    )
    .toBeGreaterThan(0);
  await page.goto(`/people/${encodeURIComponent(nsid)}/photos`);
  const listing = JSON.parse((await page.locator('body').textContent()) ?? '{}') as {
    items: Array<{ id: number; owner_nsid: string }>;
  };
  expect(listing.items.length).toBeGreaterThan(0);
  expect(listing.items.every((photo) => photo.owner_nsid === nsid)).toBe(true);
  const photo = listing.items[0];
  expect(photo).toBeDefined();
  await page.goto(`/photos/${String(photo?.id)}`);
  await expect(page.locator('body')).toContainText(nsid);
  await expectNoErrorPage(page, '/photos from crawl');
  watch.expectClean('/catalog observations');
});
