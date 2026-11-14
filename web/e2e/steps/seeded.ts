import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

/** Steps that provoke one kind of page problem on purpose, so the monitor behind every journey can be shown to notice it. */
export async function serveHtmlAt(page: Page, path: string): Promise<void> {
  await page.route(`**${path}`, (route) =>
    route.fulfill({ status: 200, contentType: 'text/html', body: '<html></html>' }),
  );
}

export async function serveStatusAt(page: Page, path: string, status: number): Promise<void> {
  await page.route(`**${path}`, (route) => route.fulfill({ status, body: '{}' }));
}

export async function refuseConnectionAt(page: Page, path: string): Promise<void> {
  await page.route(`**${path}`, (route) => route.abort('connectionrefused'));
}

export async function fetchJsonInPage(page: Page, path: string): Promise<void> {
  await page.evaluate((target) => fetch(target, { headers: { Accept: 'application/json' } }), path);
}

export async function fetchEachInPage(page: Page, paths: string[]): Promise<void> {
  await page.evaluate((targets) => Promise.all(targets.map((target) => fetch(target))), paths);
}

export async function fetchRefusedInPage(page: Page, path: string): Promise<void> {
  await page.evaluate((target) => fetch(target).catch(() => undefined), path);
}

export async function raiseConsoleErrorAndException(page: Page): Promise<void> {
  await page.evaluate(() => {
    console.error('seeded console error');
    setTimeout(() => {
      throw new Error('seeded exception');
    });
  });
}

export async function expectProblemCountAtLeast(read: () => number, count: number): Promise<void> {
  await expect.poll(read).toBeGreaterThanOrEqual(count);
}
