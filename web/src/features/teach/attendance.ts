import type { TeachLesson } from '@/api/queries';

/**
 * Where a lesson's attendance stands for the caller: `open` (it may be
 * taken now), `taken` (nobody is left whose attendance is unknown), or
 * `missing` (an instructor's or accompanist's day of the lesson is over and
 * attendance was not taken; the office takes it now). None for a lesson that
 * has not started or that the school canceled.
 */
export type AttendanceState = 'open' | 'taken' | 'missing';

/**
 * The attendance state of a lesson at `now` (the server's clock, in ms),
 * as setAttendance decides it: from the lesson's start, for instructors and
 * accompanists until `dayOverAt`, for the office and admins (`office`) at
 * any time, on their own lessons too. A lesson without active bookings
 * counts as taken. The list may be older than the start: before it, every
 * active booking is still unknown.
 */
export function attendanceState(
  lesson: TeachLesson,
  now: number,
  office: boolean,
): AttendanceState | undefined {
  if (lesson.status === 'canceled' || now < Date.parse(lesson.startsAt)) return undefined;
  if (lesson.attendanceTaken || lesson.active === 0) return 'taken';
  if (office || now < Date.parse(lesson.dayOverAt)) return 'open';
  return 'missing';
}
