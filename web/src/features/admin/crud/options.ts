import type { Location, Room, StaffEntry } from '@/api/queries';
import type { components } from '@/api/schema';

/** An option of a SelectField. */
export type Option = { value: string; label: string };

/**
 * The options of a staff select: the active staff of `kind` and, so an
 * existing record keeps them, its `current` member even when inactive.
 * `inactive` labels an inactive member.
 */
export function staffOptions(
  staff: StaffEntry[],
  kind: StaffEntry['kind'],
  current: components['schemas']['StaffRef'] | undefined,
  inactive: (name: string) => string,
): Option[] {
  const options = staff
    .filter((s) => s.id === current?.id || (s.kind === kind && s.active))
    .map((s) => ({ value: s.id, label: s.active ? s.displayName : inactive(s.displayName) }));
  if (current && !options.some((o) => o.value === current.id)) {
    options.push({ value: current.id, label: current.displayName });
  }
  return options;
}

/**
 * The options of a room select, labelled "<location> · <room>" in the API's
 * order (by location, then the office's order of its rooms); `unknown` names
 * a location that is not in the list.
 */
export function roomOptions(rooms: Room[], locations: Location[], unknown: string): Option[] {
  const locationName = new Map(locations.map((l) => [l.id, l.name]));
  return rooms.map((r) => ({
    value: r.id,
    label: `${locationName.get(r.locationId) ?? unknown} · ${r.name}`,
  }));
}
