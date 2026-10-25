import { expect } from '@playwright/test';

import { mockApi, test } from './support';

test.beforeEach(async ({ page }) => {
  await mockApi(page, 'admin');
});

test('an admin opening a user URL stays there and keeps the query string', async ({ page }) => {
  await page.goto('/crawl?tab=queue');

  await expect(page.getByTestId('shell-nav')).toBeVisible();
  await expect(page).toHaveURL(/\/crawl\?tab=queue$/);
  await page.reload();
  await expect(page.getByTestId('shell-nav')).toBeVisible();
  await expect(page).toHaveURL(/\/crawl\?tab=queue$/);
});

test('an admin returning from the Flickr OAuth callback sees the result and the URL is cleaned', async ({
  page,
}) => {
  await page.goto('/keys?connected=7');

  await expect(page.getByTestId('keys-connect-notice')).toBeVisible();
  await expect(page).toHaveURL(/\/keys$/);

  await page.goto('/keys?connect_error=invalid_state');
  await expect(page.getByTestId('keys-connect-notice')).toHaveAttribute('role', 'alert');
  await expect(page).toHaveURL(/\/keys$/);
});

test('the User switch is remembered and only changes where / lands', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveURL(/\/admin$/);

  await page.getByTestId('admin-role-switch').getByRole('button', { name: 'User' }).click();
  await expect(page).toHaveURL(/\/$/);

  await page.goto('/');
  await expect(page.getByTestId('shell-nav')).toBeVisible();
  await expect(page).toHaveURL(/\/$/);

  await page.goto('/admin/users');
  await expect(page.getByTestId('admin-rail')).toBeVisible();
  await expect(page).toHaveURL(/\/admin\/users$/);
});
