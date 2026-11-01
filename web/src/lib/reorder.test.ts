import { describe, expect, it } from 'vitest';
import { moveItem } from './reorder';

describe('moveItem', () => {
  const items = ['a', 'b', 'c'] as const;

  it('moves an item one place up', () => {
    expect(moveItem(items, 1, -1)).toEqual(['b', 'a', 'c']);
  });

  it('moves an item one place down', () => {
    expect(moveItem(items, 1, 1)).toEqual(['a', 'c', 'b']);
  });

  it('leaves the first item up and the last item down where they are', () => {
    expect(moveItem(items, 0, -1)).toEqual(['a', 'b', 'c']);
    expect(moveItem(items, 2, 1)).toEqual(['a', 'b', 'c']);
  });

  it('leaves the list alone for an index outside it', () => {
    expect(moveItem(items, 3, -1)).toEqual(['a', 'b', 'c']);
    expect(moveItem(items, -1, 1)).toEqual(['a', 'b', 'c']);
  });

  it('returns a new array and keeps the given one', () => {
    const given = ['a', 'b'];
    const moved = moveItem(given, 0, 1);
    expect(moved).not.toBe(given);
    expect(given).toEqual(['a', 'b']);
  });
});
