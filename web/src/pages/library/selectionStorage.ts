import { SESSION_SCOPED_KEY_PREFIX } from '../../shared/sessionScoped';

const KEY_PREFIX = `${SESSION_SCOPED_KEY_PREFIX}library.selection.`;

const keyFor = (userId: number): string => `${KEY_PREFIX}${String(userId)}`;

/**
 * Reads the selection kept for this user in the browser session. Selections of any other user left in
 * the same tab (a previous sign-in) are deleted, so photo IDs and counts never leak to the next user.
 * Storage can be unavailable or hold garbage, in which case the selection is empty.
 */
export function loadSelection(userId: number): Set<number> {
  try {
    const own = keyFor(userId);
    for (let index = window.sessionStorage.length - 1; index >= 0; index -= 1) {
      const key = window.sessionStorage.key(index);
      if (key !== null && key.startsWith(KEY_PREFIX) && key !== own) {
        window.sessionStorage.removeItem(key);
      }
    }
    const raw = window.sessionStorage.getItem(own);
    if (raw === null) return new Set();
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Set();
    return new Set(
      parsed.filter(
        (id): id is number => typeof id === 'number' && Number.isSafeInteger(id) && id > 0,
      ),
    );
  } catch {
    return new Set();
  }
}

/** Keeps the selection for this user and browser session so it survives leaving and re-opening the Library. */
export function saveSelection(userId: number, selected: ReadonlySet<number>): void {
  try {
    const own = keyFor(userId);
    if (selected.size === 0) window.sessionStorage.removeItem(own);
    else window.sessionStorage.setItem(own, JSON.stringify([...selected]));
  } catch {
    // Private mode or a full quota: the selection then lives only in memory, as before.
  }
}
