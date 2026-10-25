import { expect, test } from '@playwright/test';
import { monitor } from './support/monitor';

// Seeded failures: the monitor behind every journey and the route sweep must notice each kind of problem.
// (The same detection caught the real bug this suite was written for: GET /keys answered with the page.)
test.describe('the monitor fails on', () => {
  test('an endpoint that answers HTML instead of JSON', async ({ page }) => {
    const watch = monitor(page);
    await page.route('**/__e2e/html', (route) =>
      route.fulfill({ status: 200, contentType: 'text/html', body: '<html></html>' }),
    );
    await page.goto('/login');
    await page.evaluate(() => fetch('/__e2e/html', { headers: { Accept: 'application/json' } }));
    expect(watch.problems.join('\n')).toContain('HTML instead of JSON: GET /__e2e/html 200');
    expect(() => watch.expectClean('seeded')).toThrow();
  });

  test('an error status that is not allow-listed', async ({ page }) => {
    const watch = monitor(page);
    await page.route('**/__e2e/missing', (route) => route.fulfill({ status: 404, body: '{}' }));
    await page.route('**/__e2e/broken', (route) => route.fulfill({ status: 500, body: '{}' }));
    await page.goto('/login');
    await page.evaluate(() => Promise.all([fetch('/__e2e/missing'), fetch('/__e2e/broken')]));
    expect(watch.problems).toEqual(
      expect.arrayContaining(['HTTP GET /__e2e/missing 404', 'HTTP GET /__e2e/broken 500']),
    );
  });

  test('a console error and an uncaught exception', async ({ page }) => {
    const watch = monitor(page);
    await page.goto('/login');
    await page.evaluate(() => {
      console.error('seeded console error');
      setTimeout(() => {
        throw new Error('seeded exception');
      });
    });
    await expect.poll(() => watch.problems.length).toBeGreaterThanOrEqual(2);
    expect(watch.problems.join('\n')).toContain('console.error: seeded console error');
    expect(watch.problems.join('\n')).toContain('uncaught exception: seeded exception');
  });

  test('a failed request', async ({ page }) => {
    const watch = monitor(page);
    await page.route('**/__e2e/refused', (route) => route.abort('connectionrefused'));
    await page.goto('/login');
    await page.evaluate(() => fetch('/__e2e/refused').catch(() => undefined));
    await expect.poll(() => watch.problems.join('\n')).toContain('request failed: GET');
  });

  test('tolerates only the responses a journey provokes', async ({ page }) => {
    const watch = monitor(page);
    await page.route('**/__e2e/provoked', (route) => route.fulfill({ status: 422, body: '{}' }));
    await page.goto('/login');
    watch.expectFailure('GET', '/__e2e/provoked', 422);
    await page.evaluate(() => fetch('/__e2e/provoked'));
    watch.expectClean('provoked failure');
  });
});
