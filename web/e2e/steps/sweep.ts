import { expect } from '@playwright/test';
import type { Page } from '@playwright/test';

export async function expectPublicScreen(page: Page, url: string): Promise<void> {
  await expect(page).toHaveURL(new RegExp(`${url}$`));
  // Registration is closed once the first user exists, and the screen then says so instead of showing the form.
  await expect(
    page.getByTestId('auth-card').or(page.getByTestId('auth-register-closed')),
  ).toBeVisible();
}

/** A visitor sees the sign-in screen or the shell's signed-out state; either way nothing may break. */
export async function expectVisitorScreen(page: Page): Promise<void> {
  await expect(
    page.getByTestId('auth-card').or(page.locator('[data-testid$="-topbar"]')),
  ).toBeVisible();
}

export async function expectNotFoundScreen(page: Page): Promise<void> {
  await expect(page.getByTestId('shell-notfound')).toBeVisible();
}

export async function expectStayedOnAddress(page: Page, url: string): Promise<void> {
  await expect(page, `${url} stayed on its address`).toHaveURL(
    new RegExp(`${url.replace(/\/$/, '')}/?$`),
  );
}

export async function applyTheme(page: Page, theme: 'light' | 'dark'): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem('xf.theme.user', value);
  }, theme);
}

export async function expectAuthTheme(page: Page, theme: string): Promise<void> {
  await expect(page.getByTestId('auth-page')).toHaveAttribute('data-theme', theme);
}

export async function expectUserTheme(page: Page, theme: string): Promise<void> {
  await expect(page.locator('[data-role="user"]').first()).toHaveAttribute('data-theme', theme);
}
