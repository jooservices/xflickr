import { expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

type PhotoPatch = {
  owner_nsid?: string;
  sizes_status?: 'none' | 'provisional' | 'confirmed' | 'unavailable';
  ai_status?: 'none' | 'ready';
};

const favorite = (id: number, patch: PhotoPatch = {}) => ({
  photo: {
    id,
    owner_nsid: patch.owner_nsid ?? 'linh@N00',
    title: `Photo ${String(id)}`,
    description: '',
    labels: [],
    visibility: 'public',
    sizes_status: patch.sizes_status ?? 'confirmed',
    sizes: [],
    ai_status: patch.ai_status ?? 'ready',
  },
  favorite_at: '2026-11-01T00:00:00Z',
});

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

async function setup(page: Page) {
  await mockApi(page, 'user');
}

test('filters loaded favorites and keeps selection without download navigation', async ({
  page,
}) => {
  await setup(page);
  let downloadRequests = 0;
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/downloads') downloadRequests += 1;
  });
  await page.route('**/favorites*', (route) => {
    const cursor = new URL(route.request().url()).searchParams.get('cursor');
    return cursor === null
      ? json(route, {
          items: [
            favorite(1),
            favorite(2, { owner_nsid: 'morgan@N00', sizes_status: 'none' }),
            favorite(3, { owner_nsid: 'morgan@N00', ai_status: 'none' }),
            favorite(4, { owner_nsid: 'morgan@N00' }),
          ],
          next_cursor: 'cursor-2',
        })
      : json(route, { items: [favorite(5)] });
  });
  await page.goto('/library');

  await expect(page.getByTestId('user-library-heading')).toHaveText('Library');
  await expect(page.getByTestId('user-library-photo-1')).toBeVisible();
  await expectNoSeriousViolations(page);
  await page.getByRole('checkbox', { name: 'morgan@N00' }).check();
  await page.getByTestId('user-library-filter-sizes').check();
  await page.getByTestId('user-library-filter-captioned').check();
  await expect(page.getByTestId('user-library-photo-4')).toBeVisible();
  await expect(page.getByTestId('user-library-photo-2')).toHaveCount(0);
  await expect(page.getByTestId('user-library-photo-3')).toHaveCount(0);
  await page.getByTestId('shell-lang-toggle').click();
  await expect(page.getByTestId('user-library-heading')).toHaveText('Thư viện');

  await page.getByTestId('user-library-select-mode').click();
  await page.getByTestId('user-library-select-visible').click();
  await expect(page.getByTestId('user-library-selection-status')).toContainText('1');
  await expect(page.getByTestId('user-library-photo-select-4')).toBeChecked();
  await page.getByTestId('user-library-filter-clear').click();
  await page.getByTestId('user-library-load-more').click();
  await expect(page.getByTestId('user-library-photo-5')).toBeVisible();
  await expect(page.getByTestId('user-library-photo-select-4')).toBeChecked();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('user-library-select-mode')).toHaveAttribute(
    'aria-pressed',
    'false',
  );
  await page.getByTestId('user-library-select-mode').click();
  await expect(page.getByTestId('user-library-selection-status')).toContainText('1');
  await expect(page.getByTestId('user-library-photo-select-4')).toBeChecked();

  await expect(page.getByTestId('user-library-download-tray')).toHaveCount(0);
  expect(downloadRequests).toBe(0);
  await expect(page).toHaveURL(/\/library$/);
});

test('opens a Library photo, navigates, and restores route and focus', async ({ page }) => {
  await setup(page);
  const browserErrors: string[] = [];
  page.on('console', (message) => {
    if (message.type() === 'error') browserErrors.push(message.text());
  });
  page.on('pageerror', (error) => browserErrors.push(error.message));
  await page.route('**/favorites*', (route) => json(route, { items: [favorite(1), favorite(2)] }));
  await page.route('**/photos/*', (route) => {
    const id = Number(new URL(route.request().url()).pathname.split('/').at(-1));
    return json(route, {
      ...favorite(id).photo,
      title: `Detail ${String(id)}`,
      description: 'Catalog description',
      labels: ['night-sky'],
      sizes: [
        {
          label: 'Medium',
          width: 800,
          height: 600,
          source: `https://images.test/${String(id)}`,
        },
      ],
    });
  });

  await page.goto('/library');
  const opener = page.getByTestId('user-library-photo-open-2');
  await opener.focus();
  await opener.press('Enter');

  const viewer = page.getByRole('dialog', { name: 'Detail 2' });
  await expect(viewer).toBeVisible();
  await expect(page.getByTestId('lightbox-position')).toHaveText('Photo 2 of 2');
  await expect(viewer).toContainText('Catalog description');
  await expect(viewer).toContainText('night-sky');
  await expect(viewer).toContainText('800 × 600');
  await expectNoSeriousViolations(page);

  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('dialog', { name: 'Detail 1' })).toBeVisible();
  await expect(page.getByTestId('lightbox-position')).toHaveText('Photo 1 of 2');
  await page.keyboard.press('Escape');

  await expect(viewer).toHaveCount(0);
  await expect(page).toHaveURL(/\/library$/);
  await expect(opener).toBeFocused();
  expect(browserErrors).toEqual([]);
});

test('keeps a 5,000-photo cursor result virtualized while scrolling', async ({ page }) => {
  await setup(page);
  const total = 5_000;
  const pageSize = 100;
  await page.route('**/favorites*', (route) => {
    const url = new URL(route.request().url());
    const start = Number(url.searchParams.get('cursor') ?? '0');
    const end = Math.min(start + pageSize, total);
    const items = Array.from({ length: end - start }, (_, index) => favorite(start + index + 1));
    return json(route, {
      items,
      ...(end < total ? { next_cursor: String(end) } : {}),
    });
  });
  await page.goto('/library');
  await expect(page.getByTestId('user-library-photo-1')).toBeVisible();
  await expect(page.getByTestId('user-library-total')).toContainText('100 favorite photos loaded');

  for (let loaded = pageSize; loaded < total; loaded += pageSize) {
    await page.getByTestId('user-library-load-more').click();
    const nextLoaded = new Intl.NumberFormat('en').format(loaded + pageSize);
    await expect(page.getByTestId('user-library-total')).toContainText(
      `${nextLoaded} favorite photos loaded`,
    );
  }

  await page.keyboard.press('End');
  await expect(page.getByTestId('user-library-photo-5000')).toBeVisible();
  const mountedCards = await page
    .getByTestId('user-library-grid')
    .locator('[data-testid^="user-library-photo-"]')
    .evaluateAll(
      (elements) =>
        elements.filter((element) =>
          /^user-library-photo-\d+$/.test(element.getAttribute('data-testid') ?? ''),
        ).length,
    );
  expect(mountedCards).toBeLessThan(50);
});
