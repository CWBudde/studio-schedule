import { levels, type ScheduleSearch } from '../search';

/** The search params that filter the schedule. */
export const filterKeys = ['location', 'instructor', 'courseType', 'level'] as const;

/** Levels in the order the filter offers them. */
export const levelOptions = levels;

/** Whether any filter param is set. */
export function hasActiveFilter(search: ScheduleSearch): boolean {
  return filterKeys.some((key) => search[key] !== undefined);
}

/** A search patch that removes every filter param. */
export const clearedFilters: Partial<ScheduleSearch> = {
  location: undefined,
  instructor: undefined,
  courseType: undefined,
  level: undefined,
};
