const STORAGE_KEY = 'xflickr.library.selection';

/** Reads the kept selection; storage can be unavailable or hold garbage, in which case it is empty. */
export function loadSelection(): Set<number> {
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY);
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

/** Keeps the selection for the browser session so it survives leaving and re-opening the Library. */
export function saveSelection(selected: ReadonlySet<number>): void {
  try {
    if (selected.size === 0) window.sessionStorage.removeItem(STORAGE_KEY);
    else window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify([...selected]));
  } catch {
    // Private mode or a full quota: the selection then lives only in memory, as before.
  }
}
