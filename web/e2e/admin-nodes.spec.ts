import { expect, test } from '@playwright/test';
import { expectNoErrorPage, monitor } from './support/monitor';

const quotaKey = (label: string) => ({
  key_id: 17,
  user_id: 29,
  label,
  node: 'reporting-node-only',
  used: 40,
  limit: 3600,
  state: 'cooling',
  trend: 'steady',
});

test('admin reads and refreshes key states', async ({ page }) => {
  const watch = monitor(page);
  let quotaCalls = 0;
  let refreshed = false;
  await page.route('**/api/v1/me', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        success: true,
        code: 200,
        message: 'OK',
        data: {
          id: 1,
          email: 'admin@example.test',
          role: 'admin',
          locale: 'en',
          created_at: '2026-01-01T00:00:00Z',
          links: { self: { href: '/api/v1/me', method: 'GET' } },
        },
        meta: {},
        errors: null,
        trace_id: 'admin-nodes-e2e',
      }),
    }),
  );
  await page.route('**/admin/quota', (route) => {
    quotaCalls += 1;
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        keys: [quotaKey(refreshed ? 'Updated key label' : 'First key label')],
      }),
    });
  });

  await page.goto('/admin/nodes');
  const panel = page.getByTestId('admin-nodes-key-placement');
  await expect(page.getByRole('heading', { level: 1, name: 'Cluster nodes' })).toBeVisible();
  await expect(panel.getByText('First key label')).toBeVisible();
  await expect(panel.getByText('#17')).toBeVisible();
  await expect(panel.getByText('User #29')).toBeVisible();
  await expect(panel.getByText('Cooling')).toBeVisible();
  await expect(panel.getByText('reporting-node-only')).toHaveCount(0);

  const initialQuotaCalls = quotaCalls;
  refreshed = true;
  await page.getByTestId('admin-nodes-refresh').click();
  await expect(panel.getByText('Updated key label')).toBeVisible();
  expect(quotaCalls).toBeGreaterThan(initialQuotaCalls);
  await page.getByTestId('admin-lang-toggle').click();
  await expect(page.getByRole('heading', { level: 1, name: 'Các node' })).toBeVisible();
  await expect(panel.getByText('Người dùng #29')).toBeVisible();

  await page.setViewportSize({ width: 390, height: 844 });
  const widths = await page.evaluate(() => ({
    viewport: document.documentElement.clientWidth,
    page: document.documentElement.scrollWidth,
  }));
  expect(widths.page).toBeLessThanOrEqual(widths.viewport);
  await expectNoErrorPage(page, '/admin/nodes');
  watch.expectClean('/admin/nodes');
});
