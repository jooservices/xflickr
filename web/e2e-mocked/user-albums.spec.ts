import { expect } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

test('filters contact albums without linking to an unavailable detail route', async ({ page }) => {
  const browserErrors: string[] = [];
  page.on('pageerror', (error) => browserErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') browserErrors.push(message.text());
  });
  await mockApi(page, 'user');
  await page.route(
    (url) => url.pathname === '/me/albums' && url.searchParams.has('limit'),
    async (route) => {
      const cursor = new URL(route.request().url()).searchParams.get('cursor');
      const items = cursor
        ? [
            {
              id: 'album-2',
              owner_nsid: 'owner-2',
              title: 'Night skies',
              description: '',
              total_count: 3,
            },
          ]
        : [
            {
              id: 'album-1',
              owner_nsid: 'owner-1',
              title: 'Vietnam 2024',
              description: '',
              total_count: 8,
            },
          ];
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ items, next_cursor: cursor ? undefined : 'next' }),
      });
    },
  );
  await page.goto('/albums');

  await expect(page.getByRole('heading', { level: 1, name: 'Albums' })).toBeVisible();
  await expect(page.getByTestId('album-card')).toHaveAttribute('data-album-id', 'album-1');
  await expectNoSeriousViolations(page);

  await page.getByTestId('albums-filter').selectOption('owner-1');
  await expect(page).toHaveURL(/contact=owner-1/);
  await expect(page.getByTestId('album-card')).not.toHaveAttribute('href');
  expect(browserErrors).toEqual([]);
});
