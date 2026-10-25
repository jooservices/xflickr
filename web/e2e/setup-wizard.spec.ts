import { expect, test } from '@playwright/test';
import { expectNoSeriousViolations, mockApi } from './support';

test('accepts a Flickr key, returns from authorization and shows crawl progress', async ({
  page,
}) => {
  await mockApi(page, 'user');
  const created = {
    id: 5,
    label: 'My Flickr key',
    key_public: '012345••••',
    per_hour_limit: 3600,
    per_second_limit: 4,
    burst: 8,
    status: 'valid',
    created_at: '2026-10-25T00:00:00Z',
  };
  let keyCreated = false;
  await page.route('**/keys', async (route) => {
    if (route.request().method() === 'GET')
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(keyCreated ? [created] : []),
      });
    keyCreated = true;
    expect(route.request().postDataJSON()).toMatchObject({
      label: 'My Flickr key',
      key: '0123456789abcdef0123456789abcdef',
      secret: '0123456789abcdef',
    });
    return route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify(created),
    });
  });
  await page.route('**/flickr-accounts/connect', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        authorize_url: `${new URL(page.url()).origin}/onboarding?state=fake-state&oauth_token=fake-token`,
      }),
    }),
  );
  const connected = {
    id: 9,
    key_id: 5,
    nsid: 'fake-user',
    username: 'Fake Flickr user',
    status: 'ok',
    created_at: '2026-10-25T00:00:00Z',
  };
  let accountConnected = false;
  await page.route('**/flickr-accounts', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(accountConnected ? [connected] : []),
    }),
  );
  await page.route('**/flickr-accounts/callback**', (route) => {
    accountConnected = true;
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(connected),
    });
  });
  await page.route('**/me/crawl/queue', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        lanes: [
          { lane: 'person', backlog_calls: 3, effective_calls_per_hour: 3600, eta_hours: 0.01 },
        ],
      }),
    }),
  );

  await page.goto('/onboarding');
  await page.getByTestId('wizard-key-input').fill('0123456789abcdef0123456789abcdef');
  await page.getByTestId('wizard-key-secret').fill('0123456789abcdef');
  await page.getByTestId('wizard-key-submit').click();
  await expect(page.getByTestId('wizard-step-connect')).toBeVisible();
  await expect(page.getByTestId('wizard-title')).toBeFocused();
  await expect(page.getByTestId('shell-crawling-off-banner')).toHaveCount(0);
  await page.getByTestId('wizard-connect-allow').click();
  await expect(page.getByTestId('wizard-step-done')).toBeVisible();
  await expect(page.getByTestId('wizard-account-fake-user')).toBeFocused();
  await expect(page).toHaveURL(/step=3&connected=9/);
  await expect(page.getByTestId('wizard-crawl-progress')).toContainText('3 calls are queued');
  await expectNoSeriousViolations(page);
});

test('shows a persistent crawling-off banner when the user has no valid key', async ({ page }) => {
  await mockApi(page, 'user');
  await page.route('**/keys', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/flickr-accounts', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.goto('/profile');
  await expect(page.getByTestId('shell-crawling-off-banner')).toBeVisible();
  await page.getByTestId('shell-banner-add-key').click();
  await expect(page).toHaveURL(/\/onboarding$/);
  await expect(page.getByTestId('wizard-step-key')).toBeVisible();
});

test('resumes at the step the server data implies and has no horizontal scroll at 390 px', async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page, 'user');
  await page.route('**/keys', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([
        {
          id: 5,
          label: 'My Flickr key',
          key_public: '012345••••',
          per_hour_limit: 3600,
          per_second_limit: 4,
          burst: 8,
          status: 'valid',
          created_at: '2026-10-25T00:00:00Z',
        },
      ]),
    }),
  );
  await page.route('**/flickr-accounts', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.goto('/onboarding?step=3');
  await expect(page.getByTestId('wizard-empty')).toBeVisible();
  await page.getByTestId('wizard-empty-cta').click();
  await expect(page.getByTestId('wizard-connect-allow')).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(overflow).toBe(false);
  await expectNoSeriousViolations(page);
});
