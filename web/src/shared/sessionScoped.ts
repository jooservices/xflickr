/**
 * Keys under this prefix hold state that belongs to one signed-in browser session (for example the
 * Library selection). Signing out removes all of them, so it can never reappear after the next sign-in,
 * whoever signs in.
 */
export const SESSION_SCOPED_KEY_PREFIX = 'xflickr.session.';

export function clearSessionScopedStorage(): void {
  try {
    for (let index = window.sessionStorage.length - 1; index >= 0; index -= 1) {
      const key = window.sessionStorage.key(index);
      if (key !== null && key.startsWith(SESSION_SCOPED_KEY_PREFIX)) {
        window.sessionStorage.removeItem(key);
      }
    }
  } catch {
    // Storage unavailable: nothing was kept.
  }
}
