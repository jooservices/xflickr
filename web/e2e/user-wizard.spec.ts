import { expect, test } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi } from './support';

const KEY = '0123456789abcdef0123456789abcdef';
const SECRET = '0123456789abcdef';

const apiKey = (id: number, label = 'My Flickr key') => ({
  id,
  label,
  key_public: '012345••••',
  per_hour_limit: 3600,
  per_second_limit: 4,
  burst: 8,
  status: 'valid',
  created_at: '2026-10-25T00:00:00Z',
});
const connected = {
  id: 9,
  key_id: 5,
  nsid: 'fake-user',
  username: 'Fake Flickr user',
  status: 'ok',
  created_at: '2026-10-25T00:00:00Z',
};

type World = {
  keys: ReturnType<typeof apiKey>[];
  accounts: (typeof connected)[];
  settings: { depth: number | null; depth_default: number; depth_max: number } | number;
  keysStatus: number;
  keyCreate: { status: number; body?: unknown }[];
  crawlStart: { status: number; body?: unknown };
  posted: { keys: unknown[]; crawl: unknown[] };
};

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

/** A stateful stand-in for the wizard's endpoints; every test starts from `overrides`. */
async function wizardApi(page: Page, overrides: Partial<World> = {}): Promise<World> {
  const world: World = {
    keys: [],
    accounts: [],
    settings: { depth: null, depth_default: 2, depth_max: 4 },
    keysStatus: 200,
    keyCreate: [],
    crawlStart: { status: 202, body: { started: true } },
    posted: { keys: [], crawl: [] },
    ...overrides,
  };
  await mockApi(page, 'user');
  const at = (path: string) => (url: URL) => url.pathname === path;

  await page.route(at('/keys'), (route) => {
    if (route.request().method() === 'GET')
      return world.keysStatus === 200
        ? json(route, world.keys)
        : json(route, { code: 'x' }, world.keysStatus);
    world.posted.keys.push(route.request().postDataJSON());
    const next = world.keyCreate.shift() ?? { status: 201 };
    if (next.status === 201) {
      const created = apiKey(world.keys.length + 5, 'My Flickr key');
      world.keys.push(created);
      return json(route, created, 201);
    }
    return next.body === 'abort' ? route.abort() : json(route, next.body ?? {}, next.status);
  });
  await page.route(at('/flickr-accounts'), (route) => json(route, world.accounts));
  await page.route(at('/flickr-accounts/connect'), (route) =>
    json(route, {
      authorize_url: `${new URL(page.url()).origin}/onboarding?state=fake-state&oauth_token=fake-token&oauth_verifier=v`,
    }),
  );
  await page.route(at('/flickr-accounts/callback'), (route) => {
    world.accounts = [connected];
    return json(route, connected);
  });
  await page.route(at('/me/crawl-settings'), (route) =>
    typeof world.settings === 'number'
      ? json(route, { code: 'x' }, world.settings)
      : json(route, world.settings),
  );
  await page.route(at('/crawl/start'), (route) => {
    world.posted.crawl.push(route.request().postDataJSON());
    return json(route, world.crawlStart.body ?? {}, world.crawlStart.status);
  });
  return world;
}

async function submitKey(page: Page, key = KEY, secret = SECRET) {
  await page.getByTestId('wizard-key-input').fill(key);
  await page.getByTestId('wizard-key-secret').fill(secret);
  await page.getByTestId('wizard-key-submit').click();
}

test('walks the three steps: key, authorization return, then starts the crawl', async ({
  page,
}) => {
  const world = await wizardApi(page);
  await page.goto('/onboarding');
  await expect(page.getByTestId('wizard-step-key')).toBeVisible();
  await expect(page).toHaveTitle(/Set up/);
  await expectNoSeriousViolations(page);

  await submitKey(page);
  await expect(page.getByTestId('wizard-step-connect')).toBeVisible();
  await expect(page.getByTestId('wizard-title')).toBeFocused();
  expect(world.posted.keys[0]).toMatchObject({ label: 'My Flickr key', key: KEY, secret: SECRET });
  await expect(page.getByTestId('shell-crawling-off-banner')).toHaveCount(0);

  await page.getByTestId('wizard-connect-allow').click();
  await expect(page.getByTestId('wizard-step-done')).toBeVisible();
  await expect(page.getByTestId('wizard-account-fake-user')).toBeFocused();
  await expect(page).toHaveURL(/step=3&connected=9/);
  await expect(page.getByTestId('wizard-depth')).toHaveValue('2');
  await expectNoSeriousViolations(page);

  await page.getByTestId('wizard-finish').click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByText('Crawling has started')).toBeVisible();
  expect(world.posted.crawl).toEqual([{ depth: 2 }]);
});

test('keeps the form and its values on bad hex, key_invalid, key_duplicate and network errors', async ({
  page,
}) => {
  await wizardApi(page, {
    keyCreate: [
      { status: 422, body: { code: 'key_invalid' } },
      { status: 409, body: { code: 'key_duplicate' } },
      { status: 500, body: 'abort' },
    ],
  });
  await page.goto('/onboarding');

  // Enter submits; a click would miss, because the error that blur adds moves the button.
  await page.getByTestId('wizard-key-input').fill('xyz');
  await page.getByTestId('wizard-key-secret').fill('abc');
  await page.getByTestId('wizard-key-secret').press('Enter');
  await expect(page.getByTestId('wizard-key-error-key')).toBeVisible();
  await expect(page.getByTestId('wizard-key-input')).toBeFocused();

  await submitKey(page);
  await expect(page.getByTestId('wizard-key-error')).toContainText('Flickr rejected this key');
  await expect(page.getByTestId('wizard-key-input')).toHaveValue(KEY);

  await page.getByTestId('wizard-key-submit').click();
  await expect(page.getByTestId('wizard-key-error-key')).toContainText('already in your account');

  await page.getByTestId('wizard-key-submit').click();
  await expect(page.getByTestId('wizard-key-error')).toContainText('could not reach Flickr');
  await expect(page.getByTestId('wizard-key-secret')).toHaveValue(SECRET);

  await page.getByTestId('wizard-key-submit').click();
  await expect(page.getByTestId('wizard-step-connect')).toBeVisible();
});

test('shows the denied message after returning from Flickr and Try again starts a new connect', async ({
  page,
}) => {
  await wizardApi(page, { keys: [apiKey(5)] });
  await page.goto('/onboarding?step=2&error=denied');
  await expect(page.getByTestId('wizard-connect-denied')).toBeVisible();
  await page.getByTestId('wizard-connect-retry').click();
  await expect(page.getByTestId('wizard-step-done')).toBeVisible();
});

test('caps the depth at depth_max and posts the chosen depth', async ({ page }) => {
  const world = await wizardApi(page, {
    keys: [apiKey(5)],
    accounts: [connected],
    settings: { depth: 9, depth_default: 2, depth_max: 3 },
  });
  await page.goto('/onboarding');
  const slider = page.getByTestId('wizard-depth');
  await expect(slider).toHaveAttribute('max', '3');
  await expect(slider).toHaveValue('3');
  await slider.fill('1');
  await expect(page.getByTestId('wizard-depth-value')).toContainText('1');
  await page.getByTestId('wizard-finish').click();
  await expect(page).toHaveURL(/\/$/);
  expect(world.posted.crawl).toEqual([{ depth: 1 }]);
});

test('keeps step 3 with a banner when the crawl cannot start', async ({ page }) => {
  await wizardApi(page, {
    keys: [apiKey(5)],
    accounts: [connected],
    crawlStart: { status: 500, body: { code: 'x' } },
  });
  await page.goto('/onboarding');
  await page.getByTestId('wizard-finish').click();
  await expect(page.getByTestId('wizard-done-error')).toBeVisible();
  await expect(page).toHaveURL(/\/onboarding/);
});

test('shows the stale settings banner and still lets the person start with depth 2', async ({
  page,
}) => {
  const world = await wizardApi(page, {
    keys: [apiKey(5)],
    accounts: [connected],
    settings: 500,
  });
  await page.goto('/onboarding');
  await expect(page.getByTestId('wizard-stale-banner')).toContainText('Depth 2 is used');
  await page.getByTestId('wizard-finish').click();
  await expect(page).toHaveURL(/\/$/);
  expect(world.posted.crawl).toEqual([{ depth: 2 }]);
});

test('adds another API key from step 3 in a modal and returns to step 2 with it', async ({
  page,
}) => {
  const world = await wizardApi(page, { keys: [apiKey(5)], accounts: [connected] });
  await page.goto('/onboarding');
  const opener = page.getByTestId('wizard-add-key');
  await opener.click();
  const dialog = page.getByTestId('wizard-addkey-modal');
  await expect(dialog).toBeVisible();
  await expect(page.getByTestId('wizard-addkey-label')).toBeFocused();
  await expectNoSeriousViolations(page);

  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(opener).toBeFocused();

  await opener.click();
  await page.getByTestId('wizard-addkey-submit').click();
  await expect(page.getByTestId('wizard-addkey-label')).toBeFocused();
  await page.getByTestId('wizard-addkey-label').fill('Second key');
  await page.getByTestId('wizard-addkey-key').fill(KEY);
  await page.getByTestId('wizard-addkey-secret').fill(SECRET);
  await page.getByTestId('wizard-addkey-submit').click();
  await expect(page.getByTestId('wizard-step-connect')).toBeVisible();
  await expect(dialog).toHaveCount(0);
  expect(world.posted.keys[0]).toMatchObject({ label: 'Second key', key: KEY, secret: SECRET });
  await expect(page.getByTestId('wizard-key-select')).toBeVisible();
});

test('loops Add another account back to step 2 and Back returns to step 3', async ({ page }) => {
  await wizardApi(page, { keys: [apiKey(5)], accounts: [connected] });
  await page.goto('/onboarding');
  await page.getByTestId('wizard-add-account').click();
  await expect(page.getByTestId('wizard-step-connect')).toBeVisible();
  await page.getByTestId('wizard-connect-back').click();
  await expect(page.getByTestId('wizard-step-key')).toBeVisible();
  await page.getByTestId('wizard-key-back').click();
  await expect(page.getByTestId('wizard-step-done')).toBeVisible();
});

test('resumes at the step the server data implies after a reload', async ({ page }) => {
  const world = await wizardApi(page, { keys: [apiKey(5)] });
  await page.goto('/onboarding');
  await expect(page.getByTestId('wizard-step-connect')).toBeVisible();
  await page.reload();
  await expect(page.getByTestId('wizard-step-connect')).toBeVisible();

  world.accounts = [connected];
  await page.reload();
  await expect(page.getByTestId('wizard-step-done')).toBeVisible();
});

test('shows the empty state on step 3 without an account and opens step 2 from it', async ({
  page,
}) => {
  await wizardApi(page, { keys: [apiKey(5)] });
  await page.goto('/onboarding?step=3');
  await expect(page.getByTestId('wizard-empty')).toBeVisible();
  await expect(page.getByTestId('wizard-finish')).toHaveCount(0);
  await page.getByTestId('wizard-empty-cta').click();
  await expect(page.getByTestId('wizard-connect-allow')).toBeVisible();
});

test('shows the load error with a retry that recovers', async ({ page }) => {
  const world = await wizardApi(page, { keysStatus: 500 });
  await page.goto('/onboarding');
  await expect(page.getByTestId('wizard-error')).toBeVisible();
  await expectNoSeriousViolations(page);
  world.keysStatus = 200;
  await page.getByTestId('wizard-error-retry').click();
  await expect(page.getByTestId('wizard-step-key')).toBeVisible();
});

test('shows the forbidden state on 403 with a link back to Home', async ({ page }) => {
  await wizardApi(page, { keysStatus: 403 });
  await page.goto('/onboarding');
  await expect(page.getByTestId('wizard-forbidden')).toBeVisible();
  await expect(page.getByTestId('wizard-error-retry')).toHaveCount(0);
  await page.getByTestId('wizard-forbidden-cta').click();
  await expect(page).toHaveURL(/\/$/);
});

test('switches between English and Vietnamese', async ({ page }) => {
  await wizardApi(page);
  await page.goto('/onboarding');
  await expect(page.getByTestId('wizard-title')).toHaveText('Bring your own Flickr API key');
  await page.getByTestId('shell-lang-toggle').click();
  await expect(page.getByTestId('wizard-title')).toHaveText('Dùng khóa API Flickr của riêng bạn');
  await expect(page.getByTestId('wizard-eyebrow')).toContainText('Bước 1/3');
});

test('fits a 390 px screen on every step without horizontal scroll', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await wizardApi(page, { keys: [apiKey(5)], accounts: [connected] });
  const noOverflow = async () =>
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
      ),
    ).toBe(false);

  await page.goto('/onboarding');
  await noOverflow();
  await page.getByTestId('wizard-add-key').click();
  await expect(page.getByTestId('wizard-addkey-modal')).toBeVisible();
  await noOverflow();
  await expectNoSeriousViolations(page);
  await page.keyboard.press('Escape');
  await page.getByTestId('wizard-add-account').click();
  await expect(page.getByTestId('wizard-connect-allow')).toBeVisible();
  await noOverflow();
  await expectNoSeriousViolations(page);
});

test('shows a persistent crawling-off banner when the user has no valid key', async ({ page }) => {
  await wizardApi(page);
  await page.goto('/profile');
  await expect(page.getByTestId('shell-crawling-off-banner')).toBeVisible();
  await page.getByTestId('shell-banner-add-key').click();
  await expect(page).toHaveURL(/\/onboarding$/);
  await expect(page.getByTestId('wizard-step-key')).toBeVisible();
});
