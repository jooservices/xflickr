import { expect, test } from '@playwright/test';
import type { Page, Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi } from './support';

const lane = (
  name: 'person' | 'sizes',
  backlog: number,
  eta: number | null,
  position: number | null,
) => ({
  lane: name,
  backlog_calls: backlog,
  effective_calls_per_hour: 360,
  eta_hours: eta,
  position,
  done_calls: 100,
});

const baseQueue = {
  state: 'running',
  resets_at: null,
  server_time: '2026-10-25T10:00:00Z',
  exhausted_keys: [],
  lanes: [lane('person', 1200, 3.34, 4), lane('sizes', 0, null, null)],
};

const contact = (id: string, patch: Record<string, unknown> = {}) => ({
  person_id: id,
  nsid: `${id}@N01`,
  name: `Contact ${id}`,
  avatar_url: null,
  depth: 1,
  starred: false,
  selected_until: null,
  paused: false,
  status: 'crawling',
  photos_crawled: 50,
  photos_total: 200,
  sizes_percent: 25,
  last_check_at: new Date(Date.now() - 600_000).toISOString(),
  ...patch,
});

const job = {
  id: 'j1',
  person_id: 'p1',
  contact_name: 'Contact p1',
  facet: 'photos',
  lane: 'person',
  state: 'failed',
  attempt: 3,
  max_attempts: 5,
  page: 3,
  pages: 9,
  created_at: '2026-10-25T09:00:00Z',
  priority: 'normal',
  key_label: 'My Flickr key',
  calls: 14,
  records: 1200,
  error: { code: 'rate_limited', message: 'flickr.photos.search: 429' },
  started_at: '2026-10-25T09:05:00Z',
  finished_at: '2026-10-25T09:06:00Z',
  next_retry_at: null,
  attempts: [{ n: 1, at: '2026-10-25T09:05:00Z', result: 'failed', duration_ms: 1500 }],
};

type World = {
  queue: Record<string, unknown> | number;
  settings: Record<string, unknown>;
  contacts: ReturnType<typeof contact>[];
  depthsStatus: number;
  allStatus: number;
  stream: string | null;
  put: { settings: unknown[]; interests: unknown[] };
  retried: number;
  settingsPut: { status: number; body?: unknown } | null;
};

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

/** A stateful stand-in for the crawl endpoints; every test starts from `overrides`. */
async function crawlApi(page: Page, overrides: Partial<World> = {}): Promise<World> {
  const world: World = {
    queue: baseQueue,
    settings: { depth: 2, default_depth: 2, max_depth: 4, paused: false },
    contacts: [contact('p1', { starred: true }), contact('p2', { depth: 2 })],
    depthsStatus: 200,
    allStatus: 200,
    stream: null,
    put: { settings: [], interests: [] },
    retried: 0,
    settingsPut: null,
    ...overrides,
  };
  await mockApi(page, 'user');
  const at = (path: string) => (url: URL) => url.pathname === path;
  const below = (prefix: string) => (url: URL) => url.pathname.startsWith(prefix);
  const gated = (route: Route, body: () => unknown) =>
    world.allStatus === 200 ? json(route, body()) : json(route, { code: 'x' }, world.allStatus);

  await page.route(at('/api/v1/events/stream'), (route) =>
    world.stream === null
      ? route.abort()
      : route.fulfill({ status: 200, contentType: 'text/event-stream', body: world.stream }),
  );
  await page.route(at('/me/crawl/queue'), (route) =>
    typeof world.queue === 'number'
      ? json(route, { code: 'x' }, world.queue)
      : gated(route, () => world.queue),
  );
  await page.route(at('/me/crawl/settings'), (route) => {
    if (route.request().method() === 'GET') return gated(route, () => world.settings);
    const body = route.request().postDataJSON() as Record<string, unknown>;
    world.put.settings.push(body);
    if (world.settingsPut !== null)
      return json(route, world.settingsPut.body ?? {}, world.settingsPut.status);
    world.settings = { ...world.settings, ...body };
    return json(route, world.settings);
  });
  await page.route(at('/me/crawl/depths'), (route) =>
    world.depthsStatus === 200
      ? gated(route, () => ({
          items: [
            {
              depth: 0,
              contacts_total: 1,
              done: 1,
              crawling: 0,
              waiting: 0,
              error: 0,
              beyond_setting: false,
            },
            {
              depth: 1,
              contacts_total: 40,
              done: 30,
              crawling: 5,
              waiting: 4,
              error: 1,
              beyond_setting: false,
            },
            {
              depth: 2,
              contacts_total: 300,
              done: 100,
              crawling: 20,
              waiting: 170,
              error: 10,
              beyond_setting: false,
            },
            {
              depth: 3,
              contacts_total: 12,
              done: 12,
              crawling: 0,
              waiting: 0,
              error: 0,
              beyond_setting: true,
            },
          ],
        }))
      : json(route, { code: 'x' }, world.depthsStatus),
  );
  await page.route(at('/me/crawl/summary'), (route) =>
    gated(route, () => ({
      recrawl_interval_hours: 24,
      contacts_done: 143,
      contacts_total: 353,
      calls_saved_week: 5200,
    })),
  );
  await page.route(at('/me/crawl/interests'), (route) => {
    const filter = new URL(route.request().url()).searchParams.get('filter');
    const items =
      filter === 'starred' ? world.contacts.filter((item) => item.starred) : world.contacts;
    return gated(route, () => ({ items, next_cursor: null }));
  });
  await page.route(below('/me/crawl/interests/'), (route) => {
    const body = route.request().postDataJSON() as Record<string, unknown>;
    world.put.interests.push(body);
    const id = decodeURIComponent(new URL(route.request().url()).pathname.split('/').pop() ?? '');
    const found = world.contacts.find((item) => item.person_id === id);
    if (found === undefined) return json(route, { code: 'not_found' }, 404);
    const next = {
      ...found,
      ...(body.selected === true
        ? { selected_until: new Date(Date.now() + 86_400_000).toISOString() }
        : {}),
      ...(body.starred === undefined ? {} : { starred: body.starred }),
      ...(body.paused === undefined ? {} : { paused: body.paused }),
    };
    world.contacts = world.contacts.map((item) => (item.person_id === id ? next : item));
    return json(route, next);
  });
  await page.route(at('/me/crawl/jobs'), (route) =>
    gated(route, () => ({ items: [job], next_cursor: null })),
  );
  await page.route(at('/me/crawl/jobs/j1'), (route) => json(route, job));
  await page.route(at('/me/crawl/jobs/j1/retry'), (route) => {
    world.retried += 1;
    return json(route, { ...job, state: 'pending' }, 202);
  });
  return world;
}

test('shows depth, lanes with ETA and queue position, depth columns, contacts and jobs', async ({
  page,
}) => {
  await crawlApi(page);
  await page.goto('/crawl');

  await expect(page).toHaveTitle('Crawl · XFlickr');
  await expect(page.getByTestId('user-crawl-title')).toBeFocused();
  await expect(page.getByTestId('user-crawl-lane-eta-person')).toContainText('~3 h 20 min');
  await expect(page.getByTestId('user-crawl-lane-position-person')).toContainText('#4');
  await expect(page.getByTestId('user-crawl-depth-column-3')).toContainText('Above setting');
  await expect(page.getByTestId('user-crawl-contact-p1')).toBeVisible();
  await expect(page.getByTestId('user-crawl-job-j1')).toContainText('Failed');
  await expectNoSeriousViolations(page);
});

test('saves the depth, resets on the cap error and pauses and resumes the crawl', async ({
  page,
}) => {
  const world = await crawlApi(page);
  await page.goto('/crawl');

  await page.getByTestId('user-crawl-depth-slider').fill('3');
  await page.getByTestId('user-crawl-depth-save').click();
  await expect(page.getByText('Depth set to 3')).toBeVisible();
  expect(world.put.settings).toEqual([{ depth: 3 }]);

  world.settingsPut = { status: 422, body: { code: 'depth_above_cap', details: { max_depth: 3 } } };
  await page.getByTestId('user-crawl-depth-slider').fill('4');
  await page.getByTestId('user-crawl-depth-save').click();
  await expect(page.getByText('Your maximum depth is 3. Pick a lower value.')).toBeVisible();
  await expect(page.getByTestId('user-crawl-depth-value')).toHaveText('Depth 3');

  world.settingsPut = null;
  await page.getByTestId('user-crawl-pause-all').click();
  await expect(page.getByTestId('user-crawl-pause-all')).toHaveText('Resume crawl');
  await page.getByTestId('user-crawl-pause-all').click();
  await expect(page.getByTestId('user-crawl-pause-all')).toHaveText('Pause crawl');
});

test('shows the four quota banners and the waiting countdown reaching zero', async ({ page }) => {
  const resetsAt = new Date(Date.now() + 3000).toISOString();
  const world = await crawlApi(page, {
    queue: {
      ...baseQueue,
      state: 'waiting_quota',
      resets_at: resetsAt,
      server_time: new Date().toISOString(),
    },
  });
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-banner-waiting')).toBeVisible();
  await expect(page.getByTestId('user-crawl-banner-countdown')).toHaveText(/00:0[0-3]/);
  world.queue = baseQueue;
  await expect(page.getByTestId('user-crawl-banner-waiting')).toHaveCount(0, { timeout: 10_000 });

  world.queue = {
    ...baseQueue,
    state: 'key_exhausted',
    exhausted_keys: [{ id: 1, label: 'Key A' }],
  };
  await page.reload();
  await expect(page.getByTestId('user-crawl-banner-exhausted')).toContainText('Key A');

  world.queue = { ...baseQueue, state: 'no_key' };
  await page.reload();
  await expect(page.getByTestId('user-crawl-banner-no-key')).toBeVisible();
  await expect(page.getByTestId('user-crawl-banner-action')).toHaveAttribute('href', '/keys?add=1');

  world.queue = { ...baseQueue, state: 'paused' };
  world.settings = { ...world.settings, paused: true };
  await page.reload();
  await expect(page.getByTestId('user-crawl-banner-paused')).toBeVisible();
  await page.getByTestId('user-crawl-banner-action').click();
  await expect(page.getByTestId('user-crawl-pause-all')).toHaveText('Pause crawl');
});

test('stars, prioritizes and pauses a contact', async ({ page }) => {
  const world = await crawlApi(page);
  await page.goto('/crawl');

  await page.getByTestId('user-crawl-contact-star-p2').click();
  await expect(page.getByTestId('user-crawl-contact-star-p2')).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await page.getByTestId('user-crawl-contact-prioritize-p2').click();
  await expect(page.getByText('Contact p2 moved to the front')).toBeVisible();
  await page.getByTestId('user-crawl-contact-pause-p2').click();
  await expect(page.getByTestId('user-crawl-contact-pause-p2')).toHaveText('Resume');
  expect(world.put.interests).toEqual([{ starred: true }, { selected: true }, { paused: true }]);
});

test('filters contacts by chip and by depth column', async ({ page }) => {
  await crawlApi(page);
  await page.goto('/crawl');

  await page.getByTestId('user-crawl-filter-starred').click();
  await expect(page).toHaveURL(/filter=starred/);
  await expect(page.getByTestId('user-crawl-contact-p2')).toHaveCount(0);
  await page.getByTestId('user-crawl-filter-all').click();
  await expect(page.getByTestId('user-crawl-contact-p2')).toBeVisible();
  await page.getByTestId('user-crawl-depth-column-2').click();
  await expect(page.getByTestId('user-crawl-depth-column-2')).toHaveAttribute(
    'aria-pressed',
    'true',
  );
});

test('opens a job from the list and by deep link, and retries a failed job', async ({ page }) => {
  const world = await crawlApi(page);
  await page.goto('/crawl');

  await page.getByTestId('user-crawl-job-j1').getByRole('button', { name: 'j1' }).click();
  await expect(page.getByTestId('user-crawl-job-drawer')).toBeVisible();
  await expect(page).toHaveURL(/job=j1/);
  await expect(page.getByTestId('user-crawl-job-drawer-error')).toContainText('slow down');
  await page.getByTestId('user-crawl-job-drawer-retry').click();
  await expect(page.getByText('Job queued again')).toBeVisible();
  expect(world.retried).toBe(1);
  await page.keyboard.press('Escape');
  await expect(page.getByTestId('user-crawl-job-drawer')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'j1' })).toBeFocused();

  await page.goto('/crawl?job=j1');
  await expect(page.getByTestId('user-crawl-job-drawer-id')).toHaveText('j1');
});

test('applies a live crawl.queue event from the stream', async ({ page }) => {
  const live = { ...baseQueue, lanes: [lane('person', 10, 0.5, 1), lane('sizes', 0, null, null)] };
  await crawlApi(page, { stream: `event: crawl.queue\ndata: ${JSON.stringify(live)}\n\n` });
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-lane-eta-person')).toContainText('~30 min');
  await expect(page.getByTestId('user-crawl-lane-position-person')).toContainText('#1');
});

test('polls the queue when the stream is down and shows the stale banner when it fails', async ({
  page,
}) => {
  await page.clock.install();
  const world = await crawlApi(page);
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-lane-eta-person')).toContainText('~3 h 20 min');

  world.queue = { ...baseQueue, lanes: [lane('person', 10, 0.5, 1), lane('sizes', 0, null, null)] };
  await page.clock.fastForward(16_000);
  await expect(page.getByTestId('user-crawl-lane-eta-person')).toContainText('~30 min');

  world.queue = 500;
  await page.clock.fastForward(16_000);
  await expect(page.getByTestId('user-crawl-stale-banner')).toBeVisible();
  world.queue = baseQueue;
  await page.getByTestId('user-crawl-stale-retry').click();
  await expect(page.getByTestId('user-crawl-stale-banner')).toHaveCount(0);
});

test('retries one failed section without losing the rest', async ({ page }) => {
  const world = await crawlApi(page, { depthsStatus: 500 });
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-section-error-depths')).toBeVisible();
  await expect(page.getByTestId('user-crawl-contact-p1')).toBeVisible();
  world.depthsStatus = 200;
  await page.getByTestId('user-crawl-section-retry-depths').click();
  await expect(page.getByTestId('user-crawl-depths')).toBeVisible();
});

test('shows the empty state with its call to action', async ({ page }) => {
  await crawlApi(page, { contacts: [] });
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-empty')).toBeVisible();
  await expect(page.getByTestId('user-crawl-empty-cta')).toHaveAttribute('href', '/accounts');
  await expectNoSeriousViolations(page);
});

test('shows the error state and retries it', async ({ page }) => {
  const world = await crawlApi(page, { allStatus: 500, depthsStatus: 500 });
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-error')).toBeVisible();
  world.allStatus = 200;
  world.depthsStatus = 200;
  await page.getByTestId('user-crawl-error-retry').click();
  await expect(page.getByTestId('user-crawl-contact-p1')).toBeVisible();
});

test('shows the forbidden state on a 403', async ({ page }) => {
  await crawlApi(page, { allStatus: 403, depthsStatus: 403 });
  await page.goto('/crawl');

  await expect(page.getByTestId('user-crawl-forbidden')).toBeVisible();
  await page.getByTestId('user-crawl-forbidden-cta').click();
  await expect(page).toHaveURL(/\/$/);
});

test('switches between English and Vietnamese', async ({ page }) => {
  await crawlApi(page);
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-title')).toHaveText('Crawl');

  await page.getByTestId('shell-lang-toggle').click();
  await expect(page.getByTestId('user-crawl-title')).toHaveText('Thu thập');
  await expect(page.getByTestId('user-crawl-lane-position-person')).toContainText(
    'Vị trí hàng đợi #4',
  );
});

test('has no horizontal scroll at 390 px and the drawer is a bottom sheet', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await crawlApi(page);
  await page.goto('/crawl');
  await expect(page.getByTestId('user-crawl-contact-p1')).toBeVisible();

  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  await page.goto('/crawl?job=j1');
  const box = await page.getByTestId('user-crawl-job-drawer').boundingBox();
  expect(box?.width).toBeLessThanOrEqual(390);
  expect(box?.y).toBeGreaterThan(0);
});
