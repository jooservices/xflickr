import { fileURLToPath } from 'node:url';

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

/**
 * One signed-in admin session shared by the scenarios that only need to be signed in. The sign-in form allows five
 * attempts per quarter hour, so scenarios reuse this session instead of signing in again. The runner deletes the
 * directory before each run.
 */
export const ADMIN_STATE = fileURLToPath(new URL('../../.e2e-state/admin.json', import.meta.url));
export const OTHER_SESSION_STATE = fileURLToPath(
  new URL('../../.e2e-state/other-session.json', import.meta.url),
);
export const LAST_SESSION_STATE = fileURLToPath(
  new URL('../../.e2e-state/last-session.json', import.meta.url),
);

/** A visitor: no cookies, no storage. */
export const VISITOR_STATE = { cookies: [], origins: [] };

/** `E2E_ANONYMOUS=1` (the deploy smoke without credentials) visits protected screens as a visitor. */
export const ANONYMOUS = process.env.E2E_ANONYMOUS === '1';

export const VIEWPORTS = {
  desktop: { width: 1280, height: 800 },
  mobile: { width: 390, height: 844 },
} as const;

/** fakeflickr's seeded member: the account its OAuth round trip returns. */
export const SEEDED_NSID = '10000000@N00';
