import { expect, test } from '@playwright/test';
import { expectNoSeriousViolations, mockApi } from './support';

const tabs = [
  ['home', '/', 'Home'],
  ['library', '/library', 'Library'],
  ['search', '/search', 'Search'],
  ['contacts', '/contacts', 'Contacts'],
  ['albums', '/albums', 'Albums'],
  ['crawl', '/crawl', 'Crawl'],
  ['keys', '/keys', 'Keys & quota'],
  ['downloads', '/downloads', 'Downloads'],
  ['sync', '/sync', 'Cloud sync'],
  ['sharing', '/sharing', 'Sharing'],
  ['profile', '/profile', 'Profile'],
] as const;

test.describe('user shell', () => {
  test.beforeEach(async ({ page }) => {
    await mockApi(page, 'user');
  });

  test('navigates every tab to its placeholder page', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByTestId('shell-nav-home')).toHaveAttribute('aria-current', 'page');

    for (const [id, path, label] of tabs.slice(1)) {
      await page.getByTestId(`shell-nav-${id}`).click();
      await expect(page).toHaveURL(new RegExp(`${path}$`));
      await expect(page.getByTestId(`shell-nav-${id}`)).toHaveAttribute('aria-current', 'page');
      await expect(page.getByRole('heading', { level: 1, name: label })).toBeFocused();
      await expect(page).toHaveTitle(`${label} · XFlickr`);
    }
  });

  for (const theme of ['light', 'dark'] as const) {
    test(`has no serious accessibility violations (${theme})`, async ({ page }) => {
      await page.goto('/library');
      await expect(page.getByTestId('shell-nav-library')).toHaveAttribute('aria-current', 'page');
      if (theme === 'dark') await page.getByTestId('shell-theme-toggle').click();
      await expect(page.locator('[data-role="user"]')).toHaveAttribute('data-theme', theme);

      await expectNoSeriousViolations(page);
    });
  }

  test('has no serious violations on every tab in Vietnamese', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('shell-lang-toggle').click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'vi');

    for (const [id] of tabs) {
      await page.getByTestId(`shell-nav-${id}`).click();
      await expect(page.getByTestId(`shell-nav-${id}`)).toHaveAttribute('aria-current', 'page');
      await expectNoSeriousViolations(page);
    }
  });

  test('keeps the language after a reload', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('shell-lang-toggle').click();
    await page.reload();

    await expect(page.getByTestId('shell-nav-library')).toHaveText('Thư viện');
  });

  test('previews loading, empty and error states and recovers on Retry', async ({ page }) => {
    await page.goto('/library');

    await page.getByTestId('shell-preview-loading').click();
    await expect(page.getByTestId('shell-skeleton')).toBeVisible();
    await page.getByTestId('shell-preview-empty').click();
    await expect(page.getByTestId('shell-empty')).toBeVisible();
    await page.getByTestId('shell-preview-error').click();
    await expect(page.getByTestId('shell-error')).toBeVisible();
    await expectNoSeriousViolations(page);
    await page.getByTestId('shell-error-retry').click();
    await expect(page.getByTestId('shell-page')).toContainText('not built yet');
  });

  test('opens and closes the avatar menu, tray and notifications with the keyboard', async ({
    page,
  }) => {
    await page.goto('/');

    await page.getByTestId('shell-avatar-button').click();
    await expect(page.getByTestId('shell-avatar-menu')).toBeVisible();
    await expectNoSeriousViolations(page);
    await page.keyboard.press('Escape');
    await expect(page.getByTestId('shell-avatar-button')).toBeFocused();

    await page.getByTestId('shell-tray-button').click();
    await expect(page.getByTestId('shell-tray-empty')).toBeVisible();
    await page.keyboard.press('Escape');

    await page.getByTestId('shell-notifications-button').click();
    await expect(page.getByTestId('shell-notifications-empty')).toBeVisible();
    await expectNoSeriousViolations(page);
    await page.keyboard.press('Escape');
    await expect(page.getByTestId('shell-notifications-button')).toBeFocused();
  });

  test('shows the 404 page for an unknown route and the 403 page for /admin', async ({ page }) => {
    await page.goto('/no-such-page');
    await expect(page.getByTestId('shell-notfound')).toBeVisible();
    await expectNoSeriousViolations(page);

    await page.goto('/admin/users');
    await expect(page.getByTestId('admin-forbidden')).toBeVisible();
    await expect(page.getByTestId('admin-rail')).toHaveCount(0);
    await expectNoSeriousViolations(page);
    await page.getByTestId('admin-forbidden-cta').click();
    await expect(page).toHaveURL(/\/$/);
  });

  test('wraps the top bar at 390 px without horizontal page scroll', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await page.goto('/');
    await expect(page.getByTestId('shell-search-input')).toBeVisible();

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
    );
    expect(overflow).toBe(false);
    await expectNoSeriousViolations(page);
  });
});

test('shows the session-expired modal when /me answers 401', async ({ page }) => {
  await mockApi(page, 'anonymous');
  await page.goto('/library');

  await expect(page.getByTestId('shell-session-modal')).toBeVisible();
  await expectNoSeriousViolations(page);
});
