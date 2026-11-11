import { describe, expect, it } from 'vitest';
import { selectVisible } from './selection';

describe('selectVisible', () => {
  it('skips selected IDs before applying the limit', () => {
    expect(selectVisible(new Set([1]), [1, 2, 3], 3)).toEqual(new Set([1, 2, 3]));
  });

  it('fills the last slot with the first unselected photo', () => {
    const current = new Set(Array.from({ length: 499 }, (_, index) => index + 1));

    const next = selectVisible(current, [499, 500, 501], 500);

    expect(next.size).toBe(500);
    expect(next.has(500)).toBe(true);
    expect(next.has(501)).toBe(false);
  });

  it('preserves hidden selections and leaves the input unchanged', () => {
    const current = new Set([1, 9]);

    const next = selectVisible(current, [1, 2, 2, 3], 3);

    expect([...next]).toEqual([1, 9, 2]);
    expect(current).toEqual(new Set([1, 9]));
    expect(next).not.toBe(current);
  });

  it('is idempotent', () => {
    const once = selectVisible(new Set([1]), [1, 2, 2, 3], 3);

    expect(selectVisible(once, [1, 2, 2, 3], 3)).toEqual(once);
  });

  it('handles empty and full sets', () => {
    expect(selectVisible(new Set(), [], 500)).toEqual(new Set());
    expect(selectVisible(new Set([1, 2]), [3], 2)).toEqual(new Set([1, 2]));
  });
});
