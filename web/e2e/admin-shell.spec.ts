import { expect, test } from '@playwright/test';
import { expectNoSeriousViolations, mockApi } from './support';

const items = [
  ['overview', '/admin', 'Overview'],
  ['nodes', '/admin/nodes', 'Cluster nodes'],
  ['quota', '/admin/quota', 'API quota'],
  ['metrics', '/admin/metrics', 'Metrics'],
  ['events', '/admin/events', 'Events & audit'],
  ['issues', '/admin/issues', 'Issues'],
  ['pipeline', '/admin/pipeline', 'Crawl pipeline'],
  ['downloads', '/admin/downloads', 'Downloads'],
  ['sync', '/admin/sync', 'Cloud sync'],
  ['ai', '/admin/ai', 'AI index'],
  ['users', '/admin/users', 'Users'],
  ['defaults', '/admin/defaults', 'Defaults'],
  ['sharing', '/admin/sharing', 'Sharing'],
  ['log', '/admin/log', 'API call log'],
] as const;

test.describe('admin shell', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page, 'admin');
  });

  test('navigates every rail item to its placeholder page', async ({ page }) => {
    await page.goto('/admin');
    await expect(page.getByTestId('admin-rail')).toBeVisible();
    await expect(page.getByTestId('admin-shell')).toHaveAttribute('data-theme', 'dark');

    // The overview is already open, so only later items move focus to the heading.
    for (const [id, path, label] of items.slice(1)) {
      await page.getByTestId(`admin-nav-${id}`).click();
      await expect(page).toHaveURL(new RegExp(`${path}$`));
      await expect(page.getByTestId(`admin-nav-${id}`)).toHaveAttribute('aria-current', 'page');
      await expect(page.getByRole('heading', { level: 1, name: label })).toBeFocused();
    }

    await page.getByTestId('admin-nav-overview').click();
    await expect(page).toHaveURL(/\/admin$/);
    await expect(page.getByTestId('admin-nav-overview')).toHaveAttribute('aria-current', 'page');
  });

  for (const theme of ['dark', 'light'] as const) {
    test(`has no serious accessibility violations on every item (${theme})`, async ({ page }) => {
      await page.goto('/admin');
      await expect(page.getByTestId('admin-cluster-status')).toHaveText('Healthy');
      if (theme === 'light') await page.getByTestId('admin-theme-toggle').click();
      await expect(page.getByTestId('admin-shell')).toHaveAttribute('data-theme', theme);

      for (const [id] of items) {
        await page.getByTestId(`admin-nav-${id}`).click();
        await expect(page.getByTestId(`admin-nav-${id}`)).toHaveAttribute('aria-current', 'page');
        await expectNoSeriousViolations(page);
      }
    });
  }

  test('shows the cluster footer, the Issues badge and the bell count', async ({ page }) => {
    await page.goto('/admin');

    await expect(page.getByTestId('admin-cluster-summary')).toHaveText('12 calls/s · 4/4 nodes');
    await expect(page.getByTestId('admin-nav-issues')).toHaveAccessibleName('Issues, 3 open');
    await expect(page.getByTestId('admin-bell-count')).toHaveText('2');
    await page.getByTestId('admin-cluster-footer').click();
    await expect(page).toHaveURL(/\/admin\/nodes$/);
  });

  test('switches language and keeps it in Vietnamese on the rail', async ({ page }) => {
    await page.goto('/admin');
    await page.getByTestId('admin-lang-toggle').click();

    await expect(page.getByTestId('admin-nav-nodes')).toContainText('Các node');
    await expectNoSeriousViolations(page);
  });

  test('opens the rail as a modal drawer at 390 px', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await page.goto('/admin');
    await expect(page.getByTestId('admin-rail')).toBeHidden();

    await page.getByTestId('admin-menu-btn').click();
    await expect(page.getByTestId('admin-rail')).toBeVisible();
    await expect(page.getByTestId('admin-rail')).toHaveAttribute('aria-modal', 'true');
    await expectNoSeriousViolations(page);

    await page.keyboard.press('Escape');
    await expect(page.getByTestId('admin-rail')).toBeHidden();
    await expect(page.getByTestId('admin-menu-btn')).toBeFocused();

    await page.getByTestId('admin-menu-btn').click();
    await page.getByTestId('admin-nav-quota').click();
    await expect(page).toHaveURL(/\/admin\/quota$/);
    await expect(page.getByTestId('admin-rail')).toBeHidden();
  });
});

test.describe('admin routes for other visitors', () => {
  test('block a non-admin with the 403 page and no admin chrome', async ({ page }) => {
    await mockApi(page, 'user');
    await page.goto('/admin/users');

    await expect(page.getByTestId('admin-forbidden')).toBeVisible();
    await expect(page.getByTestId('admin-rail')).toHaveCount(0);
    await expect(page.getByTestId('admin-role-switch')).toHaveCount(0);
  });

  test('send a signed-out visitor to the login page', async ({ page }) => {
    await mockApi(page, 'anonymous');
    await page.goto('/admin/users');

    await expect(page).toHaveURL(/\/login\?next=%2Fadmin%2Fusers$/);
  });
});
