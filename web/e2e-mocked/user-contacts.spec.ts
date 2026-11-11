import { expect } from '@playwright/test';
import type { Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

const contact = {
  nsid: '12345@N00',
  username: 'linh.frames',
  real_name: 'Linh Tran',
  avatar_url: null,
  cover_photo_url: null,
  location: 'Hanoi',
  depth: 2,
  found_via: [{ nsid: 'root@N00', label: 'Root account' }],
  starred: false,
  paused: false,
  crawl_status: 'running',
  photo_records_crawled: 130,
  photo_records_total: 341,
  sizes_completed: 60,
  facets: [
    { name: 'photos', completed: 130, total: 341, status: 'running', updated_at: null },
    { name: 'favorites', completed: 20, total: 27, status: 'partial', updated_at: null },
    { name: 'albums', completed: 3, total: 3, status: 'complete', updated_at: null },
    { name: 'galleries', completed: 1, total: 1, status: 'complete', updated_at: null },
    { name: 'groups', completed: 4, total: 5, status: 'running', updated_at: null },
  ],
  counts: { photos: 341, favorites: 27, albums: 3, galleries: 1, groups: 5 },
  updated_at: null,
};
const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
const image = {
  id: '771',
  title: 'Rainy street',
  thumbnail_url: null,
  width: 640,
  height: 426,
  taken_at: null,
  sizes_available: true,
};

test('searches and stars a contact, then uses contact actions and its download dialog', async ({
  page,
}) => {
  await mockApi(page, 'user');
  const requests: { path: string; body?: unknown }[] = [];
  await page.route(
    (url) => url.pathname === '/me/crawl/interests',
    (route) =>
      json(route, {
        items: [
          {
            ...contact,
            name: contact.real_name,
            photos_crawled: 130,
            photos_total: 341,
            favorites_count: 27,
            last_check_at: null,
            recent_photos: [],
          },
        ],
        counts: { all: 1, starred: 0, crawling: 1, needs_attention: 0 },
        next_cursor: null,
        updated_at: null,
      }),
  );
  await page.route(
    (url) => url.pathname === '/me/crawl/settings',
    (route) => json(route, { depth: 3, depth_default: 2, depth_max: 4 }),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00',
    (route) => json(route, contact),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00/photos',
    (route) => json(route, { items: [image], total: 341, next_cursor: null, updated_at: null }),
  );
  await page.route(
    (url) => url.pathname.endsWith('/star'),
    async (route) => {
      requests.push({
        path: new URL(route.request().url()).pathname,
        body: route.request().postDataJSON(),
      });
      await route.fulfill({ status: 204 });
    },
  );
  await page.route(
    (url) => url.pathname.endsWith('/sizes-priority'),
    async (route) => {
      requests.push({ path: new URL(route.request().url()).pathname });
      await json(route, { prioritised: 12 });
    },
  );
  await page.route(
    (url) => url.pathname.endsWith('/select'),
    async (route) => {
      requests.push({ path: new URL(route.request().url()).pathname });
      await route.fulfill({ status: 204 });
    },
  );
  await page.route(
    (url) => url.pathname === '/me/crawl/pause',
    async (route) => {
      requests.push({
        path: new URL(route.request().url()).pathname,
        body: route.request().postDataJSON(),
      });
      await route.fulfill({ status: 204 });
    },
  );
  await page.route(
    (url) => url.pathname === '/downloads',
    async (route) => {
      requests.push({ path: '/downloads', body: route.request().postDataJSON() });
      await json(
        route,
        {
          id: 9,
          kind: 'contact',
          subject: contact.nsid,
          status: 'pending',
          created_at: new Date().toISOString(),
          expires_at: new Date().toISOString(),
          item_count: 1,
          skipped: 0,
        },
        201,
      );
    },
  );

  await page.goto('/contacts');
  await expect(page.getByTestId('user-contacts-title')).toHaveText('Contacts');
  await page.getByTestId('user-contacts-search').fill('linh');
  await page.getByTestId('user-contacts-sort').selectOption('name');
  await page.getByTestId('user-contact-star').click();
  await expect(page.getByTestId('user-contact-star')).toHaveAttribute('aria-pressed', 'true');
  await page.getByRole('link', { name: 'View Linh Tran' }).click();

  await expect(page.getByTestId('user-contact-title')).toHaveText('Linh Tran');
  await expect(page.getByTestId('user-contact-photos-facet-photos')).toContainText('130');
  await expect(page.getByTestId('user-contact-photos-grid')).toContainText('Rainy street');
  await page.getByTestId('user-contact-photos-prioritise-sizes').click();
  await expect(
    page.getByRole('status').filter({ hasText: 'Prioritised 12 photo sizes.' }),
  ).toBeVisible();
  await page.getByTestId('user-contact-photos-prioritise-contact').click();
  await expect(
    page.getByRole('status').filter({ hasText: 'Contact prioritised for seven days.' }),
  ).toBeVisible();
  await page.getByTestId('user-contact-photos-crawl-toggle').click();
  await page.getByTestId('user-contact-photos-download-all').click();
  await expect(page.getByTestId('contact-download-modal')).toBeVisible();
  await page.getByTestId('download-modal-submit').click();
  await expect(page.getByTestId('contact-download-modal')).toContainText(
    'Your download has been queued',
  );
  expect(requests.map((request) => request.path)).toEqual([
    '/me/contacts/12345@N00/star',
    '/me/contacts/12345@N00/sizes-priority',
    '/me/contacts/12345@N00/select',
    '/me/crawl/pause',
    '/downloads',
  ]);
  expect(requests.at(-1)?.body).toEqual({
    kind: 'contact',
    subject: contact.nsid,
    photo_ids: [Number(image.id)],
  });
  await expectNoSeriousViolations(page);
});

test('shows each contact section and downloads favorites with contact scope', async ({ page }) => {
  await mockApi(page, 'user');
  const downloads: unknown[] = [];
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00',
    (route) => json(route, contact),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00/photos',
    (route) => json(route, { items: [image], total: 341, next_cursor: null, updated_at: null }),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00/favorites',
    (route) =>
      json(route, {
        items: [
          { ...image, owner_nsid: 'owner@N00', owner_name: 'Photo owner', owner_avatar_url: null },
        ],
        total: 27,
        next_cursor: null,
        updated_at: null,
      }),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00/albums',
    (route) =>
      json(route, {
        items: [
          {
            id: 'album-1',
            title: 'Travel',
            owner_name: contact.real_name,
            stored_count: 6,
            total_count: 8,
            cover_photo_urls: [],
          },
        ],
        total: 3,
        updated_at: null,
      }),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00/galleries',
    (route) =>
      json(route, {
        items: [{ id: 'gallery-1', title: 'Best of 2026', photo_count: 12 }],
        total: 1,
        updated_at: null,
      }),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00/groups',
    (route) =>
      json(route, {
        items: [{ nsid: 'group-1', name: 'Street', members_count: 250, pool_crawl_enabled: false }],
        total: 5,
        updated_at: null,
      }),
  );
  await page.route(
    (url) => url.pathname === '/me/contacts/12345@N00/about',
    (route) =>
      json(route, {
        last_change_check_at: null,
        facets: contact.facets,
        tombstones: { count: 2, updated_at: null },
        updated_at: null,
      }),
  );
  await page.route(
    (url) => url.pathname === '/downloads',
    async (route) => {
      downloads.push(route.request().postDataJSON());
      await json(
        route,
        {
          id: 10,
          kind: 'favorites',
          subject: contact.nsid,
          status: 'pending',
          created_at: new Date().toISOString(),
          expires_at: new Date().toISOString(),
          item_count: 1,
          skipped: 0,
        },
        201,
      );
    },
  );

  await page.goto('/contacts/12345@N00?tab=favorites');
  await expect(page.getByTestId('user-contact-favorites-grid')).toContainText('Rainy street');
  await expect(page.getByTestId('user-contact-favorites-owner-771')).toHaveText('Photo owner');
  await page.getByTestId('user-contact-favorites-download-all').click();
  await page.getByTestId('download-modal-submit').click();
  await expect(page.getByTestId('contact-download-modal')).toContainText(
    'Your download has been queued',
  );

  await page.getByTestId('user-contact-tab-albums').click();
  await expect(page.getByTestId('user-contact-albums-grid')).toContainText('Travel');
  await page.getByTestId('user-contact-tab-galleries').click();
  await expect(page.getByTestId('user-contact-galleries-grid')).toContainText('Best of 2026');
  await page.getByTestId('user-contact-tab-groups').click();
  await expect(page.getByTestId('user-contact-groups-table')).toContainText('Street');
  await page.getByTestId('user-contact-tab-about').click();
  await expect(page.getByText('Flickr NSID')).toBeVisible();
  expect(downloads).toEqual([
    { kind: 'favorites', subject: contact.nsid, photo_ids: [Number(image.id)] },
  ]);
  await expectNoSeriousViolations(page);
});
