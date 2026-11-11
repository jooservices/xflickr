import { expect } from '@playwright/test';
import type { Route } from '@playwright/test';
import { expectNoSeriousViolations, mockApi, test } from './support';

const json = (route: Route, body: unknown, status = 200) =>
  route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

test('contacts depth and retry', async ({ page }) => {
  const consoleErrors: string[] = [];
  let requests = 0;
  let recovered = false;
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text());
  });
  page.on('pageerror', (error) => consoleErrors.push(error.message));

  await mockApi(page, 'user');
  await page.route(
    (url) => url.pathname === '/me/crawl/settings',
    (route) => {
      requests += 1;
      return !recovered
        ? json(route, {
            depth: 'invalid',
            depth_default: 2,
            depth_max: 4,
            detail: 'raw server failure',
          })
        : json(route, { depth: 3, depth_default: 2, depth_max: 4 });
    },
  );
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/contacts');

  await expect(page.getByTestId('user-contacts-title')).toHaveText('Contacts');
  await expect(page.getByTestId('user-contacts-error')).toContainText('Could not load contacts');
  await expect(page.getByTestId('user-contacts-error')).not.toContainText('raw server failure');
  const requestsBeforeRetry = requests;
  recovered = true;
  await page.getByTestId('user-contacts-retry').focus();
  await page.keyboard.press('Enter');

  await expect(page.getByTestId('user-contacts-description')).toContainText('Crawl depth: 3.');
  await expect(page.getByTestId('user-contacts-unavailable')).toContainText(
    'The contacts list is not available yet.',
  );
  expect(requests).toBeGreaterThan(requestsBeforeRetry);
  await expect(page.getByTestId('user-contacts-page').getByRole('link')).toHaveCount(0);

  await page.getByTestId('shell-lang-toggle').click();
  await expect(page.getByTestId('user-contacts-title')).toHaveText('Liên hệ');
  await expect(page.getByTestId('user-contacts-description')).toContainText('Độ sâu thu thập: 3.');
  await expect(page.getByTestId('user-contacts-unavailable')).toContainText(
    'Danh sách liên hệ hiện chưa khả dụng.',
  );
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
  await expectNoSeriousViolations(page);
  expect(consoleErrors).toEqual([]);
});
