import { expect } from '@playwright/test';
import type { Browser, BrowserContext, Locator, Page } from '@playwright/test';

export async function expectProfilePage(page: Page): Promise<void> {
  await expect(page.getByTestId('profile-page')).toBeVisible();
}

export async function expectProfileEmail(page: Page, email: string): Promise<void> {
  await expect(page.getByTestId('profile-account-email')).toContainText(email);
}

export async function expectPasswordCard(page: Page): Promise<void> {
  await expect(page.getByTestId('profile-password-card')).toBeVisible();
}

export async function expectAnySessionRow(page: Page): Promise<void> {
  await expect(page.getByTestId(/^profile-session-row-/).first()).toBeVisible();
}

/** A second signed-in browser context (another session of the same member) with its profile page open. */
export async function openOtherSession(
  browser: Browser,
  origin: string,
  storageState: string,
): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ baseURL: origin, storageState });
  const page = await context.newPage();
  await page.goto('/profile');
  await expect(page.getByTestId('profile-page')).toBeVisible();
  return { context, page };
}

export async function closeContexts(contexts: BrowserContext[]): Promise<void> {
  await Promise.all(contexts.map((context) => context.close()));
}

function sessionRows(page: Page): Locator {
  return page.locator('li[data-testid^="profile-session-row-"]');
}

export async function expectSessionRowCount(page: Page, count: number): Promise<void> {
  await expect(sessionRows(page)).toHaveCount(count);
}

/** The test id of the first listed session that is not the current one. */
export async function firstOtherSessionRow(page: Page): Promise<string> {
  const rows = sessionRows(page);
  for (let index = 0; index < (await rows.count()); index += 1) {
    const row = rows.nth(index);
    if ((await row.getByTestId('profile-session-current').count()) === 0) {
      const rowTestId = await row.getAttribute('data-testid');
      if (!rowTestId) throw new Error('A session row must have a test id');
      expect(rowTestId).toMatch(/^profile-session-row-/);
      return rowTestId;
    }
  }
  throw new Error('A non-current session should be listed');
}

export async function revokeSession(page: Page, rowTestId: string): Promise<void> {
  const sessionID = rowTestId.replace('profile-session-row-', '');
  await page.getByTestId(`profile-session-revoke-${sessionID}`).click();
}

export async function confirmDialog(page: Page): Promise<void> {
  await page.getByTestId('profile-confirm-ok').click();
}

export async function expectSessionRowGone(page: Page, rowTestId: string): Promise<void> {
  await expect(page.getByTestId(rowTestId)).toHaveCount(0);
}

export async function clickRevokeOthers(page: Page): Promise<void> {
  await page.getByTestId('profile-sessions-revoke-others').click();
}

export async function expectOnlyCurrentSession(page: Page): Promise<void> {
  await expect(sessionRows(page).first().getByTestId('profile-session-current')).toBeVisible();
}

export async function fillPasswordForm(page: Page, current: string, next: string): Promise<void> {
  await page.getByTestId('profile-password-current').fill(current);
  await page.getByTestId('profile-password-new').fill(next);
  await page.getByTestId('profile-password-confirm').fill(next);
}

/** Submits the password form and returns the status the server answered. */
export async function submitPasswordChange(page: Page): Promise<number> {
  const response = page.waitForResponse(
    (candidate) =>
      candidate.url().endsWith('/api/v1/me/password') && candidate.request().method() === 'PUT',
  );
  await page.getByTestId('profile-password-submit').click();
  return (await response).status();
}

export async function currentMemberStatus(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const profile = await fetch('/api/v1/me', { credentials: 'same-origin' });
    return profile.status;
  });
}

export async function expectPasswordFormCleared(page: Page): Promise<void> {
  await expect(page.getByTestId('profile-password-current')).toHaveValue('');
  await expect(page.getByTestId('profile-password-new')).toHaveValue('');
  await expect(page.getByTestId('profile-password-confirm')).toHaveValue('');
}

export async function chooseLanguage(page: Page, language: string): Promise<void> {
  await page.getByTestId('profile-account-language').selectOption(language);
}

export async function saveAccount(page: Page): Promise<void> {
  await page.getByTestId('profile-account-save').click();
}
