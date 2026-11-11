import { expect } from '@playwright/test';
import { existsSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { APIRequestContext, Browser, Page } from '@playwright/test';

/** The admin the stack is bootstrapped with. `make e2e` generates the password; the smoke run takes both from the environment. */
export function adminCredentials(): { email: string; password: string } {
  const email = process.env.E2E_ADMIN_EMAIL;
  const password = process.env.E2E_ADMIN_PASSWORD;
  if (!email || !password) throw new Error('E2E_ADMIN_EMAIL and E2E_ADMIN_PASSWORD are required');
  return { email, password };
}

export const fakeFlickrUrl = (): string => {
  const url = process.env.E2E_FAKEFLICKR_URL;
  if (!url) throw new Error('E2E_FAKEFLICKR_URL is required');
  return url;
};

/** The signed-in app: the user shell, or the admin shell where an admin lands. */
export async function expectShell(page: Page): Promise<void> {
  await expect(
    page.locator('[data-testid="shell-topbar"], [data-testid="admin-topbar"]'),
  ).toBeVisible();
}

/** Signs in through the sign-in form and waits for the user shell. */
export async function signIn(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login');
  await page.getByTestId('auth-login-email').fill(email);
  await page.getByTestId('auth-login-password').fill(password);
  await page.getByTestId('auth-login-submit').click();
  await expectShell(page);
}

/** Reads the API from Chromium so Secure same-origin cookies behave as they do in the app. */
export async function apiRead(
  page: Page,
  path: string,
): Promise<{ status: number; body: unknown }> {
  return browserRequest(page, 'GET', path);
}

/** A write call with the CSRF header the app sends (the token is the readable cookie set at sign-in). */
export async function apiWrite(
  page: Page,
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  data?: unknown,
): Promise<{ status: number; body: unknown }> {
  return browserRequest(page, method, path, data);
}

async function browserRequest(
  page: Page,
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  data?: unknown,
): Promise<{ status: number; body: unknown }> {
  return page.evaluate(
    async ({ requestMethod, requestPath, body }) => {
      const csrfCookie = document.cookie
        .split(';')
        .map((cookie) => cookie.trim())
        .find((cookie) => cookie.startsWith('xf_csrf='));
      const csrf = csrfCookie ? decodeURIComponent(csrfCookie.slice('xf_csrf='.length)) : '';
      const headers = new Headers({ Accept: 'application/json' });
      if (requestMethod !== 'GET' && csrf) headers.set('X-CSRF-Token', csrf);
      if (body !== undefined) headers.set('Content-Type', 'application/json');
      const response = await fetch(requestPath, {
        method: requestMethod,
        headers,
        credentials: 'same-origin',
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
      });
      const text = await response.text();
      return {
        status: response.status,
        body: text === '' ? undefined : (JSON.parse(text) as unknown),
      };
    },
    { requestMethod: method, requestPath: path, body: data },
  );
}

/** Points fakeflickr at a scenario (it replaces the previous one). */
export async function setFlickrScenario(
  request: APIRequestContext,
  scenario: Record<string, unknown>,
): Promise<void> {
  const response = await request.post(`${fakeFlickrUrl()}/__admin/scenarios`, { data: scenario });
  expect(response.status(), 'fakeflickr scenario').toBe(204);
}

/**
 * One signed-in admin session shared by the specs that only need to be signed in. The sign-in form allows five
 * attempts per quarter hour, so specs reuse this session instead of signing in again. The runner deletes the
 * directory before each run.
 */
export const ADMIN_STATE = fileURLToPath(new URL('../../.e2e-state/admin.json', import.meta.url));
export const OTHER_SESSION_STATE = fileURLToPath(
  new URL('../../.e2e-state/other-session.json', import.meta.url),
);
export const LAST_SESSION_STATE = fileURLToPath(
  new URL('../../.e2e-state/last-session.json', import.meta.url),
);

export async function saveAdminState(page: Page): Promise<void> {
  await saveSessionState(page, ADMIN_STATE);
}

/** Saves an independent signed-in context for journeys that manage other sessions. */
export async function saveSessionState(page: Page, path: string): Promise<void> {
  mkdirSync(dirname(path), { recursive: true });
  await page.context().storageState({ path });
}

/** Signs in once (when no session was saved yet) and saves the session for the specs that follow. */
export async function ensureAdminState(
  browser: Browser,
  baseURL: string | undefined,
): Promise<void> {
  if (existsSync(ADMIN_STATE)) return;
  const context = await browser.newContext({ ...(baseURL === undefined ? {} : { baseURL }) });
  const page = await context.newPage();
  const { email, password } = adminCredentials();
  await signIn(page, email, password);
  await saveAdminState(page);
  await context.close();
}
