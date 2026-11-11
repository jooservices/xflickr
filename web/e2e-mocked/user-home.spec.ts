import { expect } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

test('shows the signed-in contact summary without placeholder links', async ({ page }) => {
  const browserErrors: string[] = [];
  page.on('pageerror', (error) => browserErrors.push(error.message));
  page.on('console', (message) => {
    if (message.type() === 'error') browserErrors.push(message.text());
  });
  await mockApi(page, 'user');
  await page.route(
    (url) => url.pathname === '/api/v1/me/home',
    (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          code: 200,
          message: 'OK',
          data: {
            generated_at: '2026-11-12T00:00:00Z',
            contacts: { total: 1234, direct: 1000, discovered: 234 },
            photos: { total: null, sizes_ready: null, captioned: null },
            unavailable_sections: ['photos', 'starred_contacts', 'activity'],
            links: { self: { href: '/api/v1/me/home', method: 'GET' } },
          },
          meta: {},
          errors: null,
          trace_id: 'test-trace',
        }),
      }),
  );
  await page.goto('/');

  await expect(page.getByRole('heading', { level: 1, name: 'Home' })).toBeVisible();
  await expect(page.getByTestId('shell-nav-home')).toBeVisible();
  await expect(page.getByTestId('user-home-stat-contacts')).toContainText('1,234');
  await expect(page.getByTestId('user-home-stat-direct')).toContainText('1,000 direct');
  await expect(page.getByTestId('user-home-stat-discovered')).toContainText('234 discovered');
  await expect(page.locator('h1')).toHaveCount(1);
  await expect(page.getByTestId('user-home-page').locator('a')).toHaveCount(0);
  await expect(page.getByTestId('user-home-page')).not.toContainText(/photo|starred|activity/i);
  await expectNoSeriousViolations(page);
  expect(browserErrors).toEqual([]);
});
