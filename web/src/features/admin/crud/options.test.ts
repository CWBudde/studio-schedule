import { describe, expect, it } from 'vitest';
import { ids, locations } from '@/test/mockApi';
import { roomOptions } from './options';

describe('roomOptions', () => {
  it("keeps the API's order of the rooms (by location, then position), not their names", () => {
    const rooms = [
      { id: 'r1', locationId: ids.locationNorth, name: 'Upstairs', capacity: 10, position: 1 },
      { id: 'r2', locationId: ids.locationNorth, name: 'Basement', capacity: 10, position: 2 },
      { id: 'r3', locationId: 'gone', name: 'Annex', capacity: 10, position: 1 },
    ];

    expect(roomOptions(rooms, locations, 'Unknown location')).toEqual([
      { value: 'r1', label: 'North Studio · Upstairs' },
      { value: 'r2', label: 'North Studio · Basement' },
      { value: 'r3', label: 'Unknown location · Annex' },
    ]);
  });
});
