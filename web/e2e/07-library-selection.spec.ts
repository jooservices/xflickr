import { expect, test } from '@playwright/test';
import { mockApi } from '../e2e-mocked/support';
import { monitor } from './support/monitor';

const favorite = (id: number, owner: string) => ({
  photo: {
    id,
    owner_nsid: owner,
    title: `Photo ${String(id)}`,
    description: '',
    labels: [],
    visibility: 'public',
    sizes_status: 'confirmed',
    sizes: [],
    ai_status: 'ready',
  },
  favorite_at: '2026-11-01T00:00:00Z',
});

test('selects visible favorites without losing filtered selection', async ({ page }) => {
  await mockApi(page, 'user');
  const watch = monitor(page);
  let downloadRequests = 0;
  page.on('request', (request) => {
    const url = new URL(request.url());
    if (request.method() === 'POST' && url.pathname === '/downloads') downloadRequests += 1;
  });
  const total = 501;
  const pageSize = 100;
  let favoritesRequests = 0;
  await page.route('**/favorites*', (route) => {
    favoritesRequests += 1;
    const url = new URL(route.request().url());
    const start = Number(url.searchParams.get('cursor') ?? '0');
    const end = Math.min(start + pageSize, total);
    const items = Array.from({ length: end - start }, (_, index) => {
      const id = start + index + 1;
      const owner = id >= 500 ? 'new@N00' : id === 1 ? 'linh@N00' : 'morgan@N00';
      return favorite(id, owner);
    });
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ items, ...(end < total ? { next_cursor: String(end) } : {}) }),
    });
  });

  try {
    await page.goto('/library');
    await expect(page.getByTestId('user-library-photo-1')).toBeVisible();
    const firstPhoto = page.getByTestId('user-library-photo-1');
    await firstPhoto.focus();
    await expect(firstPhoto.getByTestId('user-library-photo-caption-1')).toBeVisible();
    await expect(page.getByTestId('user-library-total')).toContainText(
      '100 favorite photos loaded',
    );

    for (let loaded = pageSize; loaded < total; loaded += pageSize) {
      await page.getByTestId('user-library-load-more').click();
      const nextLoaded = Math.min(loaded + pageSize, total);
      await expect(page.getByTestId('user-library-total')).toContainText(
        `${String(nextLoaded)} favorite photos loaded`,
      );
    }
    expect(favoritesRequests).toBe(6);

    await page.getByTestId('user-library-heading').scrollIntoViewIfNeeded();
    await page.getByTestId('user-library-select-mode').click();
    await page.getByTestId('user-library-photo-select-1').check();
    await page.getByTestId('user-library-select-visible').click();
    await expect(page.getByTestId('user-library-selection-status')).toContainText('500');
    await expect(
      page.getByText(
        'The selection limit is 500 photos. Selected photos have not been added to the download tray.',
      ),
    ).toBeVisible();

    await page.getByRole('checkbox', { name: 'new@N00' }).check();
    await expect(page.getByTestId('user-library-photo-select-500')).toBeChecked();
    await expect(page.getByTestId('user-library-photo-select-501')).not.toBeChecked();
    await expect(page.getByTestId('user-library-photo-select-501')).toBeDisabled();

    await page.keyboard.press('Escape');
    await expect(page.getByTestId('user-library-select-mode')).toHaveAttribute(
      'aria-pressed',
      'false',
    );
    await page.getByTestId('user-library-select-mode').click();
    await expect(page.getByTestId('user-library-selection-status')).toContainText('500');
    await expect(page.getByTestId('user-library-photo-select-500')).toBeChecked();
    await expect(page.getByTestId('user-library-photo-select-501')).not.toBeChecked();
    await page.getByTestId('user-library-clear-selection').click();
    await expect(page.getByTestId('user-library-selection-status')).toContainText('0');

    await expect(page.getByTestId('user-library-download-tray')).toHaveCount(0);
    expect(favoritesRequests).toBe(6);
    expect(downloadRequests).toBe(0);
    watch.expectClean('/library selection');
  } finally {
    watch.stop();
  }
});
