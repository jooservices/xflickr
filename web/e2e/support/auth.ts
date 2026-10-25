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

/** A write call with the CSRF header the app sends (the token is the readable cookie set at sign-in). */
export async function apiWrite(
  page: Page,
  method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  data?: unknown,
): Promise<{ status: number; body: unknown }> {
  const cookies = await page.context().cookies();
  const csrf = cookies.find((cookie) => /csrf/i.test(cookie.name))?.value ?? '';
  const response = await (page.request as APIRequestContext).fetch(path, {
    method,
    headers: { Accept: 'application/json', 'X-CSRF-Token': csrf },
    ...(data === undefined ? {} : { data }),
  });
  const text = await response.text();
  return {
    status: response.status(),
    body: text === '' ? undefined : (JSON.parse(text) as unknown),
  };
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

export async function saveAdminState(page: Page): Promise<void> {
  mkdirSync(dirname(ADMIN_STATE), { recursive: true });
  await page.context().storageState({ path: ADMIN_STATE });
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
