import { expect } from '@playwright/test';
import type { Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

const person = {
  nsid: '12345@N00',
  username: 'linh.frames',
  realname: 'Linh Tran',
  icon: '',
  photos_count: 2481,
};

const json = (route: Route, body: unknown) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

test('opens a real-data contact header from the paginated contacts list', async ({ page }) => {
  await mockApi(page, 'user');
  await page.route(
    (url) => url.pathname === '/people',
    (route) => json(route, { items: [person] }),
  );
  await page.route(
    (url) => url.pathname === '/people/12345@N00',
    (route) => json(route, person),
  );
  await page.route(
    (url) => url.pathname === '/me/crawl/settings',
    (route) => json(route, { depth: 3, depth_default: 2, depth_max: 4 }),
  );
  await page.route(
    (url) => url.pathname === '/me/crawl',
    (route) =>
      json(route, {
        depths: [{ depth: 1, contacts: 12 }],
        facets: [{ facet: 'photos', total: 341, done: 130, active: 20, failed: 2 }],
        lanes: [],
      }),
  );

  await page.goto('/contacts');
  await expect(page.getByTestId('user-contacts-title')).toHaveText('Contacts');
  await page.getByRole('link', { name: 'View Linh Tran' }).click();

  await expect(page.getByTestId('user-contact-title')).toHaveText('Linh Tran');
  await expect(page.getByTestId('user-contact-photo-count')).toContainText('2,481');
  await expect(page.getByTestId('user-contact-crawl')).toContainText('130 of 341 complete');
  await expectNoSeriousViolations(page);
});
