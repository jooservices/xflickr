import { describe, expect, it } from 'vitest';
import { buildJustifiedRows } from './layout';

describe('buildJustifiedRows', () => {
  it('returns no rows for an empty list', () => {
    expect(buildJustifiedRows([], 1200)).toEqual([]);
  });

  it('preserves item order and fills completed rows to the available width', () => {
    const items = [
      { id: 1, ratio: 1.5 },
      { id: 2, ratio: 1 },
      { id: 3, ratio: 0.75 },
      { id: 4, ratio: 2 },
      { id: 5, ratio: 1.25 },
    ];
    const rows = buildJustifiedRows(items, 900, 190, 10);

    expect(rows.flatMap((row) => row.tiles.map(({ item }) => item.id))).toEqual([1, 2, 3, 4, 5]);
    for (const row of rows.slice(0, -1)) {
      const width =
        row.tiles.reduce((sum, tile) => sum + tile.width, 0) + 10 * (row.tiles.length - 1);
      expect(width).toBeCloseTo(900, 5);
    }
    expect(rows.at(-1)?.height).toBeLessThanOrEqual(190);
  });

  it('keeps ratios safe and groups a 5,000-photo result into virtual rows', () => {
    const items = Array.from({ length: 5_000 }, (_, index) => ({
      id: index + 1,
      ratio: index % 11 === 0 ? Number.NaN : 0.55 + (index % 7) / 4,
    }));
    const rows = buildJustifiedRows(items, 1200);

    expect(rows.length).toBeGreaterThan(1);
    expect(rows.length).toBeLessThan(items.length);
    expect(rows.flatMap((row) => row.tiles)).toHaveLength(5_000);
    expect(rows.every((row) => Number.isFinite(row.height) && row.height > 0)).toBe(true);
  });
});
