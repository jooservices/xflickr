import { expect, test } from '@playwright/test';
import { expectNoSeriousViolations, mockApi } from '../e2e-mocked/support';
import { expectLayout } from './support/layout';
import { monitor } from './support/monitor';

const plan = {
  id: 8,
  name: 'Studio',
  weight: 3,
  max_depth_cap: 4,
  max_contacts: 1200,
  ai_quota_per_day: 2500,
  zip_ttl_hours: 48,
  resource_share: 0.375,
};

test('admin reviews and refreshes plans', async ({ page }) => {
  await mockApi(page, 'admin');
  const watch = monitor(page);
  let contacts = 1200;
  await page.route('**/admin/plans', (route) =>
    route.fulfill({ json: [{ ...plan, max_contacts: contacts }] }),
  );
  await page.goto('/admin/defaults');
  const row = page.getByTestId('admin-defaults-plan-Studio');
  await expect(row).toContainText('1,200');
  await expect(row).toContainText('37.5%');
  await expect(page.getByRole('table', { name: 'Plans' })).toBeVisible();
  contacts = 2400;
  await page.getByTestId('admin-defaults-refresh').click();
  await expect(row).toContainText('2,400');
  await expectNoSeriousViolations(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(
    page.getByRole('navigation', { name: 'Admin sections', includeHidden: true }),
  ).toBeHidden();
  await expectLayout(page, 'admin defaults mobile');
  await expect(row).toBeVisible();
  expect(await row.evaluate((element) => getComputedStyle(element).display)).toBe('grid');
  const cell = row.locator('[data-label="Contacts"]');
  expect(await cell.evaluate((element) => getComputedStyle(element, '::before').content)).toBe(
    '"Contacts"',
  );
  await expectNoSeriousViolations(page);
  await page.getByTestId('admin-lang-toggle').click();
  await expect(row).toContainText('2.400');
  await expectLayout(page, 'admin defaults mobile Vietnamese');
  watch.expectClean('admin defaults');
  watch.stop();
});

test('USER cannot enter the admin defaults route', async ({ page }) => {
  await mockApi(page, 'user');
  let calls = 0;
  await page.route('**/admin/plans', (route) => {
    calls++;
    return route.fulfill({ json: [plan] });
  });
  await page.goto('/admin/defaults');
  await expect(page.getByTestId('shell-forbidden')).toBeVisible();
  await expect(page.getByTestId('admin-defaults-page')).toHaveCount(0);
  expect(calls).toBe(0);
});
