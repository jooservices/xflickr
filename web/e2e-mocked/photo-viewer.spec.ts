import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';
import { expectNoSeriousViolations, test } from './support';

function photo(id: number) {
  return {
    id,
    owner_nsid: 'owner@example',
    title: `Photo ${String(id)}`,
    description: '',
    labels: [],
    visibility: 'public',
    sizes_status: 'provisional',
    sizes: [{ label: 'Large', url: `/mock-photo-${String(id)}.svg`, media: 'photo' }],
    ai_status: 'none',
  };
}

async function mockPhotos(page: Page) {
  const requested: number[] = [];
  await page.route('**/photos/*', async (route) => {
    const id = Number(new URL(route.request().url()).pathname.split('/').at(-1));
    requested.push(id);
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(photo(id)),
    });
  });
  await page.route('**/mock-photo-*.svg', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'image/svg+xml',
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="800" height="600"><rect width="800" height="600" fill="#5680ad"/></svg>',
    }),
  );
  return requested;
}

test('opens the viewer, navigates with the keyboard, and restores focus on Escape', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const requested = await mockPhotos(page);
  await page.goto('/e2e-mocked/photo-viewer.html');
  const originalUrl = page.url();
  const historyLength = await page.evaluate(() => history.length);
  await page.evaluate(() => window.scrollTo(0, 80));
  const originalScrollY = await page.evaluate(() => window.scrollY);
  await page.getByTestId('gallery-open').click();

  await expect(page.getByTestId('photo-lightbox')).toBeVisible();
  await expect(page.getByTestId('lightbox-close')).toBeFocused();
  await expect(page.getByTestId('lightbox-image')).toHaveAttribute('alt', 'Photo 101');
  await expect(page.getByTestId('lightbox-position')).toHaveText('Photo 1 of 2');
  await expect
    .poll(() =>
      page.getByTestId('photo-lightbox').evaluate((dialog) => {
        const panel = dialog.querySelector('aside');
        return panel ? getComputedStyle(panel).width : 'missing';
      }),
    )
    .toBe('400px');
  expect(await page.evaluate(() => window.scrollY)).toBe(originalScrollY);
  expect(requested).toEqual([101]);
  await expectNoSeriousViolations(page);

  await page.keyboard.press('Shift+Tab');
  await expect(page.getByTestId('lightbox-next')).toBeFocused();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByTestId('lightbox-image')).toHaveAttribute('alt', 'Photo 202');
  await expect(page.getByTestId('lightbox-position')).toHaveText('Photo 2 of 2');
  expect(requested).toEqual([101, 202]);

  await page.keyboard.press('ArrowLeft');
  await expect(page.getByTestId('lightbox-image')).toHaveAttribute('alt', 'Photo 101');
  await expect(page.getByTestId('lightbox-position')).toHaveText('Photo 1 of 2');
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('photo-lightbox')).toHaveCount(0);
  await expect(page.getByTestId('gallery-open')).toBeFocused();
  expect(page.url()).toBe(originalUrl);
  expect(await page.evaluate(() => history.length)).toBe(historyLength);
  expect(await page.evaluate(() => window.scrollY)).toBe(originalScrollY);
});

test('reflows at 390px without horizontal scrolling', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockPhotos(page);
  await page.goto('/e2e-mocked/photo-viewer.html');
  await page.getByTestId('gallery-open').click();

  await expect(page.getByTestId('lightbox-image')).toBeVisible();
  await expect(page.getByTestId('lightbox-next')).toBeVisible();
  const width = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(width).toBeLessThanOrEqual(390);
  await expectNoSeriousViolations(page);
});
