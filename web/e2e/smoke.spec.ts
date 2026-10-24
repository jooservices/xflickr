import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';

test('home page shows the app title and has no accessibility violations', async ({ page }) => {
  await page.goto('/');

  await expect(page).toHaveTitle('XFlickr');
  await expect(page.getByRole('heading', { level: 1, name: 'XFlickr' })).toBeVisible();

  const results = await new AxeBuilder({ page }).analyze();
  expect(results.violations).toEqual([]);
});
