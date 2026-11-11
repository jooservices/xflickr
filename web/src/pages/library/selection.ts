/** Adds loaded, visible photo IDs to a copy of the current selection up to the limit. */
export function selectVisible(
  current: ReadonlySet<number>,
  visibleIds: readonly number[],
  limit: number,
): Set<number> {
  const selected = new Set(current);
  for (const id of visibleIds) {
    if (selected.has(id)) continue;
    if (selected.size >= limit) break;
    selected.add(id);
  }
  return selected;
}
