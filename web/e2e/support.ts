import AxeBuilder from '@axe-core/playwright';
import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

type Role = 'user' | 'admin';

const summary = { unread_notifications: 3, running_downloads: 2, tray_count: 12, attention: [] };

const adminShell = {
  open_issues: 3,
  unread_notifications: 2,
  cluster: {
    status: 'ok',
    nodes_total: 4,
    nodes_healthy: 4,
    calls_per_second: 12,
    calls_history: Array.from({ length: 60 }, (_, index) => (index * 7) % 23),
  },
  generated_at: '2026-10-25T10:00:00Z',
};

const endpoints = [
  '/me',
  '/shell/summary',
  '/admin/shell',
  '/tray',
  '/notifications',
  '/healthz',
  '/keys',
  '/me/crawl/queue',
];

/** Answers the endpoints the shells read, so the browser needs no backend. */
export async function mockApi(page: Page, role: Role | 'anonymous'): Promise<void> {
  await page.route(
    (url) => endpoints.includes(url.pathname),
    async (route) => {
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
        case '/shell/summary':
          return json(summary);
        case '/admin/shell':
          return role === 'admin' ? json(adminShell) : json({ code: 'forbidden' }, 403);
        case '/tray':
          return json({ items: [], count: 0, estimated_bytes: 0 });
        case '/notifications':
          return json({ items: [] });
        case '/keys':
          return json([]);
        case '/me/crawl/queue':
          return json({ lanes: [] });
        default:
          return json({ status: 'ok' });
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
