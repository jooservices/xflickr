import { expect, test } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi } from './support';

const KEY = '0123456789abcdef0123456789abcdef';
const SECRET = '0123456789abcdef';

const apiKey = (id: number, extra: Record<string, unknown> = {}) => ({
  id,
  label: `Key ${String(id)}`,
  key_public: '012345••••',
  per_hour_limit: 3600,
  per_second_limit: 4,
  burst: 8,
  status: 'valid',
  created_at: '2026-10-25T00:00:00Z',
  ...extra,
});
const usage = (id: number, extra: Record<string, unknown> = {}) => ({
  key_id: id,
  used: 2140,
  person_used: 1500,
  sizes_used: 640,
  reserve: 100,
  burst_now: 2,
  window_resets_at: '2099-01-01T00:20:00Z',
  hourly: Array.from({ length: 24 }, (_, index) => (index === 3 ? 3600 : index * 100)),
  node_id: 'node-a',
  node_ip: '10.0.0.7',
  paused: false,
  state: 'ok',
  ...extra,
});
const flickr = (extra: Record<string, unknown> = {}) => ({
  id: 9,
  key_id: 1,
  nsid: 'fake-user',
  username: 'Fake Flickr user',
  status: 'ok',
  created_at: '2026-10-25T00:00:00Z',
  ...extra,
});

type World = {
  keys: ReturnType<typeof apiKey>[];
  usage: ReturnType<typeof usage>[];
  accounts: ReturnType<typeof flickr>[];
  keysStatus: number;
  usageStatus: number;
  create: { status: number; body?: unknown }[];
  puts: unknown[];
  deletes: string[];
  connects: unknown[];
};

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

/** A stateful stand-in for the endpoints of the Keys page. */
async function keysApi(page: Page, overrides: Partial<World> = {}): Promise<World> {
  const world: World = {
    keys: [],
    usage: [],
    accounts: [],
    keysStatus: 200,
    usageStatus: 200,
    create: [],
    puts: [],
    deletes: [],
    connects: [],
    ...overrides,
  };
  await mockApi(page, 'user');
  const at = (path: string) => (url: URL) => url.pathname === path;
  const item = (url: URL) => Number(url.pathname.split('/').at(-1));

  await page.route(at('/keys'), (route) => {
    if (route.request().resourceType() === 'document') return route.fallback();
    if (route.request().method() === 'GET')
      return world.keysStatus === 200
        ? json(route, world.keys)
        : json(route, { code: 'x' }, world.keysStatus);
    const next = world.create.shift() ?? { status: 201 };
    if (next.status !== 201) return json(route, next.body ?? {}, next.status);
    const created = apiKey(world.keys.length + 1, { label: 'Mine' });
    world.keys.push(created);
    world.usage.push(usage(created.id, { used: 0 }));
    return json(route, created, 201);
  });
  await page.route(at('/keys/usage'), (route) =>
    world.usageStatus === 200
      ? json(route, { server_time: '2099-01-01T00:00:00Z', items: world.usage })
      : json(route, { code: 'x' }, world.usageStatus),
  );
  await page.route(
    (url) => /^\/keys\/\d+$/.test(url.pathname),
    (route) => {
      const id = item(new URL(route.request().url()));
      if (route.request().method() === 'PUT') {
        const body = route.request().postDataJSON() as { paused: boolean };
        world.puts.push(body);
        world.usage = world.usage.map((entry) =>
          entry.key_id === id ? { ...entry, paused: body.paused } : entry,
        );
        return json(route, apiKey(id));
      }
      world.deletes.push(String(id));
      world.keys = world.keys.filter((entry) => entry.id !== id);
      return route.fulfill({ status: 204 });
    },
  );
  await page.route(at('/flickr-accounts'), (route) => json(route, world.accounts));
  await page.route(at('/flickr-accounts/connect'), (route) => {
    world.connects.push(route.request().postDataJSON());
    return json(route, { authorize_url: `${new URL(page.url()).origin}/accounts?fake=1` });
  });
  return world;
}

async function fillAdd(page: Page, key = KEY, secret = SECRET) {
  await page.getByTestId('keys-add-label').fill('Mine');
  await page.getByTestId('keys-add-key').fill(key);
  await page.getByTestId('keys-add-secret').fill(secret);
  await page.getByTestId('keys-add-submit').click();
}

test('empty state, then adds a key and shows its card', async ({ page }) => {
  const world = await keysApi(page);
  await page.goto('/keys');
  await expect(page.getByTestId('keys-empty')).toBeVisible();
  await expect(page).toHaveTitle('Keys & quota · XFlickr');
  await expectNoSeriousViolations(page);

  await page.getByTestId('keys-empty-cta').click();
  await expect(page.getByTestId('keys-add-modal')).toBeVisible();
  await expect(page.getByTestId('keys-add-label')).toBeFocused();
  await fillAdd(page);

  await expect(page.getByTestId('keys-add-modal')).toHaveCount(0);
  await expect(page.getByTestId('keys-card-1')).toBeVisible();
  await expect(page.getByTestId('keys-card-1').getByTestId('keys-card-label')).toBeFocused();
  await expect(page.getByTestId('keys-toast')).toContainText('placed on node-a');
  await expect(page.getByTestId('keys-accounts-connect-1')).toBeVisible();
  expect(world.keys).toHaveLength(1);
});

test('keeps the modal open on bad hex, key_invalid and key_duplicate', async ({ page }) => {
  await keysApi(page, {
    keys: [apiKey(1)],
    usage: [usage(1)],
    create: [
      { status: 422, body: { code: 'key_invalid' } },
      { status: 409, body: { code: 'key_duplicate' } },
    ],
  });
  await page.goto('/keys?add=1');
  await expect(page.getByTestId('keys-add-modal')).toBeVisible();

  await page.getByTestId('keys-add-label').fill('Mine');
  await page.getByTestId('keys-add-key').fill('xyz');
  await page.getByTestId('keys-add-secret').fill('abc');
  await page.getByTestId('keys-add-secret').press('Enter');
  await expect(page.getByTestId('keys-add-error-key')).toBeVisible();
  await expect(page.getByTestId('keys-add-error-secret')).toBeVisible();
  await expect(page.getByTestId('keys-add-key')).toBeFocused();

  await page.getByTestId('keys-add-key').fill(KEY);
  await page.getByTestId('keys-add-secret').fill(SECRET);
  await page.getByTestId('keys-add-secret').press('Enter');
  await expect(page.getByTestId('keys-add-error')).toContainText('Flickr did not accept');
  await expect(page.getByTestId('keys-add-modal')).toBeVisible();
  await expect(page.getByTestId('keys-add-secret')).toHaveValue(SECRET);

  await page.getByTestId('keys-add-secret').press('Enter');
  await expect(page.getByTestId('keys-add-error-key')).toContainText('already added');
  await page.getByTestId('keys-add-cancel').click();
  await expect(page.getByTestId('keys-add-modal')).toHaveCount(0);
  await expect(page).toHaveURL(/\/keys$/);
});

test('pauses and resumes, and removes only after confirmation', async ({ page }) => {
  const world = await keysApi(page, { keys: [apiKey(1)], usage: [usage(1)] });
  await page.goto('/keys');

  await page.getByTestId('keys-card-pause').click();
  await expect(page.getByTestId('keys-card-paused')).toBeVisible();
  await page.getByTestId('keys-card-pause').click();
  await expect(page.getByTestId('keys-card-paused')).toHaveCount(0);
  expect(world.puts).toEqual([{ paused: true }, { paused: false }]);

  await page.getByTestId('keys-card-remove').click();
  await expect(page.getByTestId('keys-remove-modal')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('keys-remove-modal')).toHaveCount(0);
  expect(world.deletes).toEqual([]);

  await page.getByTestId('keys-card-remove').click();
  await page.getByTestId('keys-remove-confirm').click();
  await expect(page.getByTestId('keys-empty')).toBeVisible();
  await expect(page.getByTestId('keys-empty-cta')).toBeFocused();
  expect(world.deletes).toEqual(['1']);
});

test('shows ok, reserve, exhausted and revoked cards and starts a reconnect', async ({ page }) => {
  const world = await keysApi(page, {
    keys: [apiKey(1), apiKey(2), apiKey(3), apiKey(4, { status: 'revoked' })],
    usage: [
      usage(1),
      usage(2, { used: 3550, state: 'reserve' }),
      usage(3, { used: 3600, state: 'exhausted' }),
      usage(4, { state: 'revoked' }),
    ],
    accounts: [flickr({ status: 'reconnect_required' })],
  });
  await page.goto('/keys');

  await expect(page.getByTestId('keys-card-1').getByTestId('keys-card-state')).toHaveText('OK');
  await expect(page.getByTestId('keys-card-2').getByTestId('keys-card-hint')).toBeVisible();
  await expect(page.getByTestId('keys-card-3').getByTestId('keys-card-state')).toHaveText(
    'Exhausted',
  );
  await expect(page.getByTestId('keys-card-4').getByTestId('keys-card-pause')).toHaveCount(0);
  await expect(page.getByTestId('keys-card-1').getByTestId('keys-card-bar')).toHaveAttribute(
    'aria-valuetext',
    '2,140 of 3,600 calls used',
  );
  await expectNoSeriousViolations(page);

  await page.getByTestId('keys-account-reconnect-fake-user').click();
  await page.waitForURL(/\/accounts\?fake=1/);
  expect(world.connects).toEqual([{ key_id: 1 }]);
});

test('warns when usage fails, shows the list error with retry and the forbidden page', async ({
  page,
}) => {
  const world = await keysApi(page, { keys: [apiKey(1)], usage: [usage(1)], usageStatus: 500 });
  await page.goto('/keys');
  await expect(page.getByTestId('keys-stale-banner')).toBeVisible();
  await expect(page.getByTestId('keys-card-used')).toContainText('–');
  world.usageStatus = 200;
  await page.getByTestId('keys-stale-retry').click();
  await expect(page.getByTestId('keys-stale-banner')).toHaveCount(0);

  world.keysStatus = 500;
  await page.reload();
  await expect(page.getByTestId('keys-error')).toBeVisible();
  world.keysStatus = 200;
  await page.getByTestId('keys-error-retry').click();
  await expect(page.getByTestId('keys-card-1')).toBeVisible();

  world.keysStatus = 403;
  await page.reload();
  await expect(page.getByTestId('keys-forbidden')).toBeVisible();
  await expect(page.getByTestId('keys-forbidden-cta')).toHaveAttribute('href', '/');
});

test('switches to Vietnamese and fits 390 px without horizontal scroll', async ({ page }) => {
  await keysApi(page, { keys: [apiKey(1)], usage: [usage(1)], accounts: [flickr()] });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/keys');
  await expect(page.getByTestId('keys-card-1')).toBeVisible();

  await page.getByTestId('shell-lang-toggle').click();
  await expect(page.getByTestId('keys-title')).toHaveText('Khóa & hạn mức');
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);

  await page.getByTestId('keys-add').click();
  await expect(page.getByTestId('keys-add-modal')).toContainText('Thêm khóa API');
});
