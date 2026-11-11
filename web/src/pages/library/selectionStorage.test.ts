import { afterEach, describe, expect, it, vi } from 'vitest';
import { loadSelection, saveSelection } from './selectionStorage';

afterEach(() => {
  window.sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('selectionStorage', () => {
  it('round-trips a selection through the session', () => {
    saveSelection(7, new Set([3, 1, 2]));
    expect([...loadSelection(7)].sort()).toEqual([1, 2, 3]);
  });

  it('removes the entry when the selection is cleared', () => {
    saveSelection(7, new Set([1]));
    saveSelection(7, new Set());
    expect(window.sessionStorage.getItem('xflickr.library.selection.7')).toBeNull();
    expect(loadSelection(7).size).toBe(0);
  });

  it('ignores malformed or foreign content', () => {
    window.sessionStorage.setItem('xflickr.library.selection.7', '{not json');
    expect(loadSelection(7).size).toBe(0);
    window.sessionStorage.setItem('xflickr.library.selection.7', '{"a":1}');
    expect(loadSelection(7).size).toBe(0);
    window.sessionStorage.setItem('xflickr.library.selection.7', '[1,"2",-3,4.5,6]');
    expect([...loadSelection(7)]).toEqual([1, 6]);
  });

  it('never shows one user the selection of another user in the same tab', () => {
    saveSelection(7, new Set([1, 2]));
    expect(loadSelection(8).size).toBe(0);
    expect(window.sessionStorage.getItem('xflickr.library.selection.7')).toBeNull();
    saveSelection(8, new Set([3]));
    expect([...loadSelection(8)]).toEqual([3]);
  });

  it('keeps working in memory when storage throws', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota');
    });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    expect(() => saveSelection(7, new Set([1]))).not.toThrow();
    expect(loadSelection(7).size).toBe(0);
  });
});
