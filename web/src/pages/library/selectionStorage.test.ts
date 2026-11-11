import { afterEach, describe, expect, it, vi } from 'vitest';
import { loadSelection, saveSelection } from './selectionStorage';

afterEach(() => {
  window.sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('selectionStorage', () => {
  it('round-trips a selection through the session', () => {
    saveSelection(new Set([3, 1, 2]));
    expect([...loadSelection()].sort()).toEqual([1, 2, 3]);
  });

  it('removes the entry when the selection is cleared', () => {
    saveSelection(new Set([1]));
    saveSelection(new Set());
    expect(window.sessionStorage.getItem('xflickr.library.selection')).toBeNull();
    expect(loadSelection().size).toBe(0);
  });

  it('ignores malformed or foreign content', () => {
    window.sessionStorage.setItem('xflickr.library.selection', '{not json');
    expect(loadSelection().size).toBe(0);
    window.sessionStorage.setItem('xflickr.library.selection', '{"a":1}');
    expect(loadSelection().size).toBe(0);
    window.sessionStorage.setItem('xflickr.library.selection', '[1,"2",-3,4.5,6]');
    expect([...loadSelection()]).toEqual([1, 6]);
  });

  it('keeps working in memory when storage throws', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota');
    });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    expect(() => saveSelection(new Set([1]))).not.toThrow();
    expect(loadSelection().size).toBe(0);
  });
});
