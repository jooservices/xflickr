export type RatioItem = { id: number; ratio: number };

export type JustifiedRow<T extends RatioItem> = {
  key: number;
  height: number;
  tiles: Array<{ item: T; width: number }>;
};

/** Packs photo ratios into rows that fill the available width. */
export function buildJustifiedRows<T extends RatioItem>(
  items: T[],
  width: number,
  targetHeight = 190,
  gap = 10,
): JustifiedRow<T>[] {
  if (items.length === 0) return [];

  const safeWidth = Math.max(1, width);
  const rows: JustifiedRow<T>[] = [];
  let pending: T[] = [];

  const makeRow = (rowItems: T[], final: boolean) => {
    const availableWidth = Math.max(1, safeWidth - gap * (rowItems.length - 1));
    const ratioSum = rowItems.reduce((sum, item) => sum + safeRatio(item.ratio), 0);
    const naturalHeight = availableWidth / ratioSum;
    const height = final ? Math.min(targetHeight, naturalHeight) : naturalHeight;
    rows.push({
      key: rowItems[0]?.id ?? rows.length,
      height,
      tiles: rowItems.map((item) => ({ item, width: safeRatio(item.ratio) * height })),
    });
  };

  for (const item of items) {
    pending.push(item);
    const availableWidth = Math.max(1, safeWidth - gap * (pending.length - 1));
    const ratioSum = pending.reduce((sum, photo) => sum + safeRatio(photo.ratio), 0);
    if (pending.length > 1 && availableWidth / ratioSum <= targetHeight) {
      makeRow(pending, false);
      pending = [];
    }
  }

  if (pending.length > 0) makeRow(pending, true);
  return rows;
}

function safeRatio(ratio: number): number {
  return Number.isFinite(ratio) && ratio > 0 ? ratio : 4 / 3;
}
