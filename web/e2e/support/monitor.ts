import { expect } from '@playwright/test';
import type { ConsoleMessage, Page, Request, Response } from '@playwright/test';
import allowlist from './allowlist.json' with { type: 'json' };

type Allowed = { method: string; path: string; status: number; reason: string };

const allowed: Allowed[] = allowlist.responses;
for (const entry of allowed) {
  if (![401, 404].includes(entry.status) || entry.reason.trim() === '') {
    throw new Error(
      `allowlist.json: ${entry.method} ${entry.path} must be a 401/404 with a reason`,
    );
  }
}

const isAllowed = (method: string, path: string, status: number) =>
  allowed.some((a) => a.method === method && a.path === path && a.status === status);

const isApiCall = (request: Request) => ['fetch', 'xhr'].includes(request.resourceType());

/** Everything a page did wrong while a screen was open. */
export type Monitor = {
  problems: string[];
  /** Fails the test when anything was collected; names the screen. */
  expectClean(screen: string): void;
  /** Forgets what was collected so far (for a step that is expected to fail, such as a rejected key). */
  reset(): void;
  /** Tolerates the next failing response for this method, path and status once (an error the journey provokes). */
  expectFailure(method: string, path: string, status: number): void;
  stop(): void;
};

/**
 * Collects uncaught exceptions, console errors, failed requests, error statuses (>= 400, minus the allow-list),
 * HTML served where JSON was expected, and the router default error page.
 */
export function monitor(page: Page): Monitor {
  const problems: string[] = [];
  const provoked: string[] = [];

  const onPageError = (error: Error) => problems.push(`uncaught exception: ${error.message}`);
  const onConsole = (message: ConsoleMessage) => {
    if (message.type() !== 'error') return;
    // Chrome logs every failing response as a console error; the response check below judges those.
    if (message.text().startsWith('Failed to load resource')) return;
    problems.push(`console.error: ${message.text()}`);
  };
  const onRequestFailed = (request: Request) => {
    const reason = request.failure()?.errorText ?? 'unknown';
    // Navigating away aborts in-flight requests; that is not a failure of the screen.
    if (reason === 'net::ERR_ABORTED') return;
    problems.push(`request failed: ${request.method()} ${request.url()} (${reason})`);
  };
  const onResponse = (response: Response) => {
    const request = response.request();
    const { pathname } = new URL(response.url());
    const key = `${request.method()} ${pathname} ${String(response.status())}`;
    if (response.status() >= 400) {
      const index = provoked.indexOf(key);
      if (index >= 0) {
        provoked.splice(index, 1);
        return;
      }
      if (!isAllowed(request.method(), pathname, response.status())) problems.push(`HTTP ${key}`);
      return;
    }
    if (!isApiCall(request)) return;
    const type = (response.headers()['content-type'] ?? '').toLowerCase();
    if (type.includes('text/html')) {
      const accept = request.headers()['accept'] ?? 'none';
      problems.push(`HTML instead of JSON: ${key} (${request.resourceType()}, Accept: ${accept})`);
    }
  };

  page.on('pageerror', onPageError);
  page.on('console', onConsole);
  page.on('requestfailed', onRequestFailed);
  page.on('response', onResponse);

  return {
    problems,
    expectClean(screen) {
      expect(problems, `problems on ${screen}`).toEqual([]);
    },
    reset() {
      problems.length = 0;
      provoked.length = 0;
    },
    expectFailure(method, path, status) {
      provoked.push(`${method} ${path} ${String(status)}`);
    },
    stop() {
      page.off('pageerror', onPageError);
      page.off('console', onConsole);
      page.off('requestfailed', onRequestFailed);
      page.off('response', onResponse);
    },
  };
}

/** The router's default error page, or the app's own error screen. */
export async function expectNoErrorPage(page: Page, screen: string): Promise<void> {
  await expect(page.getByTestId('global-error'), `error page on ${screen}`).toHaveCount(0);
  await expect(
    page.getByText('Unexpected Application Error'),
    `router error on ${screen}`,
  ).toHaveCount(0);
}
