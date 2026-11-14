import { expect } from '@playwright/test';
import type { Browser, Page } from '@playwright/test';
import { existsSync, mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { ADMIN_STATE, adminCredentials } from '../support/env';

/** The signed-in app: the user shell, or the admin shell where an admin lands. */
export async function expectShell(page: Page): Promise<void> {
  await expect(
    page.locator('[data-testid="shell-topbar"], [data-testid="admin-topbar"]'),
  ).toBeVisible();
}

export async function openPage(page: Page, path: string): Promise<void> {
  await page.goto(path);
}

export async function reloadPage(page: Page): Promise<void> {
  await page.reload();
}

export async function waitForUrl(page: Page, pattern: RegExp): Promise<void> {
  await page.waitForURL(pattern);
}

export async function expectUrl(page: Page, pattern: RegExp): Promise<void> {
  await expect(page).toHaveURL(pattern);
}

export async function waitForIdleNetwork(page: Page): Promise<void> {
  await page.waitForLoadState('networkidle');
}

export async function setViewport(
  page: Page,
  size: { width: number; height: number },
): Promise<void> {
  await page.setViewportSize(size);
}

export async function currentOrigin(page: Page): Promise<string> {
  return new URL(page.url()).origin;
}

export async function openLogin(page: Page): Promise<void> {
  await page.goto('/login');
}

export async function fillLoginEmail(page: Page, email: string): Promise<void> {
  await page.getByTestId('auth-login-email').fill(email);
}

export async function fillLoginPassword(page: Page, password: string): Promise<void> {
  await page.getByTestId('auth-login-password').fill(password);
}

export async function submitLogin(page: Page): Promise<void> {
  await page.getByTestId('auth-login-submit').click();
}

export async function expectLoginForm(page: Page): Promise<void> {
  await expect(page.getByTestId('auth-login-form')).toBeVisible();
}

export async function expectLoginError(page: Page): Promise<void> {
  await expect(page.getByTestId('auth-error-banner')).toBeVisible();
}

export async function expectLoginEmail(page: Page, email: string): Promise<void> {
  await expect(page.getByTestId('auth-login-email')).toHaveValue(email);
}

export async function expectExpiredBanner(page: Page): Promise<void> {
  await expect(page.getByTestId('auth-expired-banner')).toBeVisible();
}

export async function openRegister(page: Page): Promise<void> {
  await page.goto('/register');
}

/** Fills the registration form; the confirmation and the terms fields exist on some builds only. */
export async function fillRegistration(page: Page, email: string, password: string): Promise<void> {
  await page.getByTestId('auth-register-email').fill(email);
  await page.getByTestId('auth-register-password').fill(password);
  const confirm = page.getByTestId('auth-register-confirm');
  if (await confirm.count()) await confirm.fill(password);
  const terms = page.getByTestId('auth-register-terms');
  if (await terms.count()) await terms.check();
}

export async function submitRegistration(page: Page): Promise<void> {
  await page.getByTestId('auth-register-submit').click();
}

export async function openAccountMenu(page: Page): Promise<void> {
  await page.getByTestId('shell-avatar-button').click();
}

export async function clickSignOut(page: Page): Promise<void> {
  await page.getByTestId('shell-avatar-sign-out').click();
}

/** Expires the session the way the server does: the session cookie goes, the CSRF cookie stays. */
export async function expireSessionCookie(page: Page): Promise<void> {
  for (const cookie of await page.context().cookies()) {
    if (!/csrf/i.test(cookie.name)) await page.context().clearCookies({ name: cookie.name });
  }
}

export async function expectSessionModal(page: Page): Promise<void> {
  await expect(page.getByTestId('shell-session-modal')).toBeVisible();
}

export async function clickSessionModalSignIn(page: Page): Promise<void> {
  await page.getByTestId('shell-session-sign-in').click();
}

/** Saves this page's signed-in session so later scenarios can start from it. */
export async function saveSession(page: Page, path: string): Promise<void> {
  mkdirSync(dirname(path), { recursive: true });
  await page.context().storageState({ path });
}

/** Signs in once (when no session was saved yet) and saves the session for the scenarios that follow. */
export async function ensureAdminSession(
  browser: Browser,
  baseURL: string | undefined,
): Promise<void> {
  if (existsSync(ADMIN_STATE)) return;
  const context = await browser.newContext({ ...(baseURL === undefined ? {} : { baseURL }) });
  const page = await context.newPage();
  const { email, password } = adminCredentials();
  await openLogin(page);
  await fillLoginEmail(page, email);
  await fillLoginPassword(page, password);
  await submitLogin(page);
  await expectShell(page);
  await saveSession(page, ADMIN_STATE);
  await context.close();
}
