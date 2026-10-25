import { expect, test } from '@playwright/test';
import { ADMIN_STATE, setFlickrScenario } from './support/auth';
import { expectNoErrorPage, monitor } from './support/monitor';

// fakeflickr's seeded member (10000000@N00) is the account the OAuth round trip returns.
const NSID = '10000000@N00';

test.use({ storageState: ADMIN_STATE });

test.describe.serial('connecting a Flickr account through fakeflickr OAuth', () => {
  test('authorizes, comes back through the callback and shows the account', async ({
    page,
    request,
  }) => {
    await setFlickrScenario(request, {});
    const watch = monitor(page);
    await page.goto('/keys');
    await expect(page.getByTestId('keys-grid')).toBeVisible();
    const connect = page.locator('[data-testid^="keys-accounts-connect-"]').first();
    await expect(connect).toBeVisible();
    // The app leaves for fakeflickr's authorize page, which redirects back to /flickr-accounts/callback.
    await connect.click();
    await page.waitForURL(/\/keys/);
    await expect(page.getByTestId(`keys-account-${NSID}`)).toBeVisible();
    await expect(page.getByTestId(`keys-account-status-${NSID}`)).toBeVisible();
    await expectNoErrorPage(page, '/keys after connecting');
    watch.expectClean('Flickr OAuth round trip');
  });
});
