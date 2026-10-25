import { expect } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

const queue = {
  lanes: [
    { lane: 'person', backlog_calls: 1200, effective_calls_per_hour: 360, eta_hours: 3.34 },
    { lane: 'sizes', backlog_calls: 0, effective_calls_per_hour: 360, eta_hours: null },
  ],
};
const status = {
  depths: [
    { depth: 0, contacts: 1 },
    { depth: 1, contacts: 40 },
    { depth: 2, contacts: 300 },
    { depth: 3, contacts: 12 },
  ],
  facets: [],
  lanes: [],
};

type World = {
  settings: { depth: number; depth_default: number; depth_max: number };
  settingsStatus: number;
  statusStatus: number;
  put: unknown[];
  putResult: { status: number; body: unknown } | null;
};

const json = (route: Route, body: unknown, code = 200) =>
  route.fulfill({ status: code, contentType: 'application/json', body: JSON.stringify(body) });

/** A stateful stand-in for the endpoints the crawl page reads. */
async function crawlApi(page: Page, overrides: Partial<World> = {}): Promise<World> {
  const world: World = {
    settings: { depth: 2, depth_default: 2, depth_max: 4 },
    settingsStatus: 200,
    statusStatus: 200,
    put: [],
    putResult: null,
    ...overrides,
  };
  await mockApi(page, 'user');
  const at = (path: string) => (url: URL) => url.pathname === path;

  await page.route(at('/me/crawl/queue'), (route) => json(route, queue));
  await page.route(at('/me/crawl'), (route) =>
    route.request().resourceType() === 'document'
      ? route.fallback()
      : world.statusStatus === 200
        ? json(route, status)
        : json(route, { code: 'x' }, world.statusStatus),
  );
  await page.route(at('/me/crawl/settings'), (route) => {
    if (route.request().method() === 'GET')
      return world.settingsStatus === 200
        ? json(route, world.settings)
        : json(route, { code: 'x' }, world.settingsStatus);
    const body = route.request().postDataJSON() as { depth: number };
    world.put.push(body);
    if (world.putResult !== null) return json(route, world.putResult.body, world.putResult.status);
    world.settings = { ...world.settings, ...body };
    return json(route, world.settings);
  });
  return world;
}

test('shows depth, lanes with ETA, depth columns and the not-available block', async ({ page }) => {
  await crawlApi(page);
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-title')).toHaveText('Crawl');
  await expect(page.getByTestId('user-crawl-lane-eta-person')).toContainText('~3 h 20 min');
  await expect(page.getByTestId('user-crawl-lane-backlog-person')).toContainText('1,200');
  await expect(page.getByTestId('user-crawl-depth-value')).toContainText('Depth 2');
  await expect(page.getByTestId('user-crawl-depth-column-3')).toContainText('Above setting');
  await expect(page.getByTestId('user-crawl-unavailable')).toContainText('Not available yet');
  await expectNoSeriousViolations(page);
});

test('saves the depth and resets on the cap error', async ({ page }) => {
  const world = await crawlApi(page);
  await page.goto('/crawl');

  await page.getByTestId('user-crawl-depth-slider').fill('3');
  await page.getByTestId('user-crawl-depth-save').click();
  await expect(page.getByText('Depth set to 3')).toBeVisible();
  expect(world.put).toEqual([{ depth: 3 }]);

  world.putResult = { status: 422, body: { code: 'depth_above_cap', details: { max_depth: 3 } } };
  await page.getByTestId('user-crawl-depth-slider').fill('4');
  await page.getByTestId('user-crawl-depth-save').click();
  await expect(page.getByText('Your maximum depth is 3. Pick a lower value.')).toBeVisible();
});

test('retries one failed section without losing the rest', async ({ page }) => {
  const world = await crawlApi(page, { statusStatus: 500 });
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-section-error-depths')).toBeVisible();
  await expect(page.getByTestId('user-crawl-depth-card')).toBeVisible();
  world.statusStatus = 200;
  await page.getByTestId('user-crawl-section-retry-depths').click();
  await expect(page.getByTestId('user-crawl-depths')).toBeVisible();
});

test('shows the error state and retries it', async ({ page }) => {
  const world = await crawlApi(page, { settingsStatus: 500, statusStatus: 500 });
  await page.route(
    (url) => url.pathname === '/me/crawl/queue',
    (route) => json(route, { code: 'x' }, 500),
  );
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-error')).toBeVisible();
  world.settingsStatus = 200;
  world.statusStatus = 200;
  await page.unroute((url) => url.pathname === '/me/crawl/queue');
  await page.route(
    (url) => url.pathname === '/me/crawl/queue',
    (route) => json(route, queue),
  );
  await page.getByTestId('user-crawl-error-retry').click();
  await expect(page.getByTestId('user-crawl-depths')).toBeVisible();
});

test('shows the forbidden state on a 403', async ({ page }) => {
  await crawlApi(page, { settingsStatus: 403, statusStatus: 403 });
  await page.route(
    (url) => url.pathname === '/me/crawl/queue',
    (route) => json(route, { code: 'forbidden' }, 403),
  );
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-forbidden')).toBeVisible();
});

test('switches to Vietnamese and fits 390 px without horizontal scroll', async ({ page }) => {
  await crawlApi(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-depths')).toBeVisible();

  await page.getByTestId('shell-lang-toggle').click();
  await expect(page.getByTestId('user-crawl-title')).toHaveText('Thu thập');
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});
