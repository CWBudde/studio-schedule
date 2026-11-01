/** `items` with the item at `index` moved one place up (-1) or down (+1); unchanged at the ends. */
export function moveItem<T>(items: readonly T[], index: number, delta: -1 | 1): T[] {
  const next = [...items];
  const to = index + delta;
  if (index < 0 || index >= items.length || to < 0 || to >= items.length) return next;
  [next[index], next[to]] = [next[to]!, next[index]!];
  return next;
}
