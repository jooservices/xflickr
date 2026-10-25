import AxeBuilder from '@axe-core/playwright';
import { expect, test as base } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import type { Page } from '@playwright/test';

type Role = 'user' | 'admin';

const endpoints = [
  '/me',
  '/healthz',
  '/keys',
  '/flickr-accounts',
  '/admin/quota',
  '/me/crawl',
  '/me/crawl/queue',
  '/me/crawl/settings',
];

const contract = readFileSync(
  fileURLToPath(new URL('../../contracts/openapi/openapi.yaml', import.meta.url)),
  'utf8',
);
const contractPaths = [...contract.matchAll(/^ {2}(\/[^:\n]+):/gm)].map(
  (match) => new RegExp(`^${match[1].replace(/\{[^}]+\}/g, '[^/]+')}$`),
);

/**
 * Every test fails when the page sends an API request to a path that is not in contracts/openapi,
 * so a screen can no longer pass against routes the backend does not serve.
 */
export const test = base.extend<{ contractGuard: undefined }>({
  contractGuard: [
    async ({ page }, use) => {
      const outside: string[] = [];
      page.on('request', (request) => {
        if (!['fetch', 'xhr', 'eventsource'].includes(request.resourceType())) return;
        const { pathname } = new URL(request.url());
        if (!contractPaths.some((pattern) => pattern.test(pathname))) outside.push(pathname);
      });
      await use(undefined);
      expect(outside, 'paths called outside contracts/openapi').toEqual([]);
    },
    { auto: true },
  ],
});

/** Answers the endpoints the shells read, so the browser needs no backend. */
export async function mockApi(page: Page, role: Role | 'anonymous'): Promise<void> {
  await page.route(
    (url) => endpoints.includes(url.pathname),
    async (route) => {
      // A page navigation to /keys is the app itself, not the API call of the same path.
      if (route.request().resourceType() === 'document') return route.fallback();
      const { pathname } = new URL(route.request().url());
      const json = (body: unknown, status = 200) =>
        route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
      switch (pathname) {
        case '/me':
          return role === 'anonymous'
            ? json({}, 401)
            : json({
                id: 1,
                email: `${role}@example.test`,
                role,
                locale: 'en',
                created_at: '2026-10-01T00:00:00Z',
              });
        case '/admin/quota':
          return role === 'admin'
            ? json({
                keys: [1, 2, 3, 4, 5, 6, 7].map((id) => ({
                  key_id: id,
                  label: `Key ${String(id)}`,
                  node: `n${String(Math.min(id, 4))}`,
                  used: id <= 4 ? 10_800 : 0,
                  limit: 3600,
                  state: id <= 4 ? 'ok' : 'cooling',
                  trend: 'steady',
                })),
              })
            : json({ code: 'forbidden' }, 403);
        case '/keys':
        case '/flickr-accounts':
          return json([]);
        case '/me/crawl/queue':
          return json({ lanes: [] });
        case '/me/crawl':
          return json({ depths: [], facets: [], lanes: [] });
        case '/me/crawl/settings':
          return json({ depth: 2, depth_default: 2, depth_max: 4 });
        default:
          return json({ status: 'ok', version: 'test' });
      }
    },
  );
}

/** Fails on serious or critical accessibility violations. */
export async function expectNoSeriousViolations(page: Page): Promise<void> {
  const results = await new AxeBuilder({ page }).analyze();
  const serious = results.violations.filter(
    (violation) => violation.impact === 'serious' || violation.impact === 'critical',
  );
  expect(
    serious.map((violation) => ({
      id: violation.id,
      impact: violation.impact,
      nodes: violation.nodes.map((node) => node.target),
    })),
  ).toEqual([]);
}
