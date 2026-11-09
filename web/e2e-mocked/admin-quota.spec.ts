import { expect } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

test.describe('admin API quota view', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page, 'admin');
  });

  test('shows live key usage and reflows quota details on mobile', async ({ page }) => {
    await page.goto('/admin/quota');

    await expect(page.getByTestId('admin-quota-key-table')).toBeVisible();
    await expect(page.getByTestId('admin-quota-key-row').first()).toContainText('Key 1');
    await expect(page.getByTestId('admin-quota-key-row').first()).toContainText('OK');
    await expect(page.getByTestId('admin-quota-node-table')).toContainText(
      'Per-node quota data is not included',
    );
    await expectNoSeriousViolations(page);

    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.getByTestId('admin-quota-key-table')).toBeVisible();
    await expectNoSeriousViolations(page);
    const scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(scrollWidth).toBeLessThanOrEqual(390);
  });
});
