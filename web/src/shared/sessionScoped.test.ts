import { afterEach, describe, expect, it } from 'vitest';
import { SESSION_SCOPED_KEY_PREFIX, clearSessionScopedStorage } from './sessionScoped';

afterEach(() => window.sessionStorage.clear());

describe('clearSessionScopedStorage', () => {
  it('removes every session-scoped key and leaves unrelated keys alone', () => {
    window.sessionStorage.setItem(`${SESSION_SCOPED_KEY_PREFIX}library.selection.7`, '[1]');
    window.sessionStorage.setItem(`${SESSION_SCOPED_KEY_PREFIX}other`, 'x');
    window.sessionStorage.setItem('unrelated', 'keep');
    clearSessionScopedStorage();
    expect(
      window.sessionStorage.getItem(`${SESSION_SCOPED_KEY_PREFIX}library.selection.7`),
    ).toBeNull();
    expect(window.sessionStorage.getItem(`${SESSION_SCOPED_KEY_PREFIX}other`)).toBeNull();
    expect(window.sessionStorage.getItem('unrelated')).toBe('keep');
  });
});
