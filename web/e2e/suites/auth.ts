import type { Browser, Page } from '@playwright/test';
import {
  ADMIN_STATE,
  LAST_SESSION_STATE,
  OTHER_SESSION_STATE,
  adminCredentials,
} from '../support/env';
import { expectNoErrorPage, monitor } from '../steps/monitor';
import {
  clickSessionModalSignIn,
  clickSignOut,
  ensureAdminSession,
  expectExpiredBanner,
  expectLoginEmail,
  expectLoginError,
  expectLoginForm,
  expectSessionModal,
  expectShell,
  expectUrl,
  expireSessionCookie,
  fillLoginEmail,
  fillLoginPassword,
  fillRegistration,
  openAccountMenu,
  openLogin,
  openPage,
  openRegister,
  saveSession,
  submitLogin,
  submitRegistration,
} from '../steps/session';
import { chooseLanguage, expectProfilePage, saveAccount } from '../steps/profile';

/** Signs in through the sign-in form and waits for the shell. */
export async function signInAs(page: Page, email: string, password: string): Promise<void> {
  await openLogin(page);
  await fillLoginEmail(page, email);
  await fillLoginPassword(page, password);
  await submitLogin(page);
  await expectShell(page);
}

/** The first registered user becomes the admin and lands signed in; the session is saved for the scenarios that follow. */
export async function registerFirstUser(page: Page): Promise<void> {
  const watch = monitor(page);
  const { email, password } = adminCredentials();
  await openRegister(page);
  await fillRegistration(page, email, password);
  await submitRegistration(page);
  await expectShell(page);
  await expectNoErrorPage(page, 'after registration');
  watch.expectClean('registration');
  await saveSession(page, ADMIN_STATE);
}

export async function signOutAndInAgain(page: Page): Promise<void> {
  const watch = monitor(page);
  const { email, password } = adminCredentials();
  await signInAs(page, email, password);
  // The admin shell has no account menu and `/` sends an admin to it; a deep link opens the member's shell.
  await openPage(page, '/profile');
  await openAccountMenu(page);
  await clickSignOut(page);
  await expectUrl(page, /\/login$/);
  await expectLoginForm(page);
  await signInAs(page, email, password);
  await expectNoErrorPage(page, 'after signing in again');
  watch.expectClean('sign-out and sign-in');
  await saveSession(page, OTHER_SESSION_STATE);
}

export async function rejectWrongPassword(page: Page): Promise<void> {
  const watch = monitor(page);
  const { email } = adminCredentials();
  await openLogin(page);
  await fillLoginEmail(page, email);
  await fillLoginPassword(page, 'not-the-password-1');
  watch.expectFailure('POST', '/api/v1/sessions', 401);
  await submitLogin(page);
  await expectLoginError(page);
  await expectLoginEmail(page, email);
  watch.expectClean('failed sign-in');
}

/** A member whose session expired is sent back to sign-in and returns after login. */
export async function recoverExpiredSession(page: Page): Promise<void> {
  const { email, password } = adminCredentials();
  await openPage(page, '/profile');
  await expectProfilePage(page);
  await expireSessionCookie(page);
  // The next write the member makes is answered 401.
  await chooseLanguage(page, 'vi');
  await saveAccount(page);
  await expectSessionModal(page);
  await clickSessionModalSignIn(page);
  await expectUrl(page, /\/login\?reason=expired&return_to=%2Fprofile$/);
  await expectExpiredBanner(page);
  await fillLoginEmail(page, email);
  await fillLoginPassword(page, password);
  await submitLogin(page);
  // The sign-in screen sends an administrator to the admin overview whatever return_to says.
  await expectShell(page);
  await saveSession(page, LAST_SESSION_STATE);
}

/** Obtains the shared admin session once, before the scenarios that only need to be signed in. */
export async function prepareAdminSession(
  browser: Browser,
  baseURL: string | undefined,
): Promise<void> {
  await ensureAdminSession(browser, baseURL);
}
