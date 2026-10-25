import { expect, test } from '@playwright/test';
import { ADMIN_STATE, setFlickrScenario } from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

const VALID_KEY = '0123456789abcdef0123456789abcdef';
const VALID_SECRET = '0123456789abcdef';
const DEAD_KEY = 'deadbeefdeadbeefdeadbeefdeadbeef';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('API keys and quota on the real stack', () => {
  test.beforeEach(async ({ request }) => {
    await setFlickrScenario(request, { invalid_keys: [DEAD_KEY] });
  });

  test('shows the empty state before any key exists', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/keys');
    await expect(page.getByTestId('keys-empty')).toBeVisible();
    await expectNoErrorPage(page, '/keys');
    watch.expectClean('/keys (empty)');
  });

  test('rejects an invalid key with a clear error and keeps what was typed', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/keys');
    await page.getByTestId('keys-empty-cta').click();
    await page.getByTestId('keys-add-label').fill('Dead key');
    await page.getByTestId('keys-add-key').fill(DEAD_KEY);
    await page.getByTestId('keys-add-secret').fill(VALID_SECRET);
    watch.expectFailure('POST', '/keys', 422);
    watch.expectFailure('POST', '/keys', 400);
    await page.getByTestId('keys-add-submit').click();
    await expect(page.getByTestId('keys-add-modal')).toBeVisible();
    await expect(
      page.getByTestId('keys-add-error').or(page.getByTestId('auth-error-key')),
    ).toBeVisible();
    await expect(page.getByTestId('keys-add-label')).toHaveValue('Dead key');
    await expect(page.getByTestId('keys-add-key')).toHaveValue(DEAD_KEY);
    await expectNoErrorPage(page, 'invalid key');
    expect(
      watch.problems.filter((problem) => !/HTTP POST \/keys /.test(problem)),
      'unexpected problems while adding an invalid key',
    ).toEqual([]);
  });

  test('adds a valid key and shows its usage', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/keys');
    await page.getByTestId('keys-empty-cta').click();
    await page.getByTestId('keys-add-label').fill('E2E key');
    await page.getByTestId('keys-add-key').fill(VALID_KEY);
    await page.getByTestId('keys-add-secret').fill(VALID_SECRET);
    await page.getByTestId('keys-add-submit').click();
    await expect(page.getByTestId('keys-grid')).toBeVisible();
    await expect(page.getByTestId('keys-card-label').first()).toHaveText('E2E key');
    await expect(page.getByTestId('keys-card-used').first()).toBeVisible();
    await expect(page.getByTestId('keys-card-bar').first()).toBeVisible();
    await expect(page.getByTestId('keys-card-state').first()).toBeVisible();
    await expectNoErrorPage(page, '/keys with a key');
    watch.expectClean('/keys with a key');
  });
});
