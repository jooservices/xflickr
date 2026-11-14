import type { Browser, BrowserContext, Page } from '@playwright/test';
import { expect } from '@playwright/test';
import { LAST_SESSION_STATE, OTHER_SESSION_STATE, adminCredentials } from '../support/env';
import { apiRead, apiWrite } from '../steps/api';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import {
  clickRevokeOthers,
  closeContexts,
  confirmDialog,
  currentMemberStatus,
  expectAnySessionRow,
  expectOnlyCurrentSession,
  expectPasswordCard,
  expectPasswordFormCleared,
  expectProfileEmail,
  expectProfilePage,
  expectSessionRowCount,
  expectSessionRowGone,
  fillPasswordForm,
  firstOtherSessionRow,
  openOtherSession,
  revokeSession,
  submitPasswordChange,
} from '../steps/profile';
import { currentOrigin, openPage } from '../steps/session';

export async function showSignedInAccount(page: Page): Promise<void> {
  const watch = monitor(page);
  const { email } = adminCredentials();
  await openPage(page, '/profile');
  await expectProfilePage(page);
  await expectProfileEmail(page, email);
  await expectPasswordCard(page);
  await expectNoErrorPage(page, '/profile');
  watch.expectClean('/profile');
}

/** Revokes one session, then signs out every other session; the other contexts are answered 401. */
export async function revokeSessions(browser: Browser, page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/profile');
  await expectAnySessionRow(page);
  const origin = await currentOrigin(page);
  const contexts: BrowserContext[] = [];
  const others: Page[] = [];
  try {
    for (const storageState of [OTHER_SESSION_STATE, LAST_SESSION_STATE]) {
      const other = await openOtherSession(browser, origin, storageState);
      contexts.push(other.context);
      others.push(other.page);
    }
    await expectSessionRowCount(page, 3);
    const rowTestId = await firstOtherSessionRow(page);
    await revokeSession(page, rowTestId);
    await confirmDialog(page);
    await expectSessionRowGone(page, rowTestId);
    await clickRevokeOthers(page);
    await confirmDialog(page);
    await expectSessionRowCount(page, 1);
    await expectOnlyCurrentSession(page);
    for (const other of others) expect((await apiRead(other, '/api/v1/me')).status).toBe(401);
    await expectNoErrorPage(page, '/profile after revoking sessions');
    watch.expectClean('/profile session revocation');
  } finally {
    await closeContexts(contexts);
  }
}

async function changePassword(page: Page, current: string, next: string): Promise<void> {
  await fillPasswordForm(page, current, next);
  expect(await submitPasswordChange(page)).toBe(204);
  expect(await currentMemberStatus(page)).toBe(200);
  await expectPasswordFormCleared(page);
}

/** Changes the password and back again; the current session stays active. */
export async function changePasswordAndKeepSession(page: Page): Promise<void> {
  const watch = monitor(page);
  const { password } = adminCredentials();
  await openPage(page, '/profile');
  await expectProfilePage(page);
  const temporaryPassword = `E2e-${Date.now()}-Password!`;
  await changePassword(page, password, temporaryPassword);
  await changePassword(page, temporaryPassword, password);
  expect((await apiRead(page, '/api/v1/me')).status).toBe(200);
  await expectNoErrorPage(page, '/profile after changing password');
  watch.expectClean('/profile password update');
}

type Sharing = { data?: { enabled?: unknown } };

/** Updates the sharing choice through the REST resource and restores it afterwards. */
export async function updateAndRestoreSharing(page: Page): Promise<void> {
  const watch = monitor(page);
  await openPage(page, '/profile');
  const origin = await currentOrigin(page);
  const current = await apiRead(page, `${origin}/api/v1/me/sharing`);
  expect(current.status).toBe(200);
  const originalEnabled = (current.body as Sharing).data?.enabled;
  if (typeof originalEnabled !== 'boolean')
    throw new Error('Sharing response must include the enabled value');
  try {
    const updated = await apiWrite(page, 'PUT', '/api/v1/me/sharing', {
      enabled: !originalEnabled,
    });
    expect(updated.status).toBe(200);
    expect((updated.body as Sharing).data?.enabled).toBe(!originalEnabled);
  } finally {
    const restored = await apiWrite(page, 'PUT', '/api/v1/me/sharing', {
      enabled: originalEnabled,
    });
    expect(restored.status).toBe(200);
    expect((restored.body as Sharing).data?.enabled).toBe(originalEnabled);
  }
  await expectNoErrorPage(page, '/profile sharing update');
  watch.expectClean('/profile sharing update');
}
