/**
 * Pure layout of the week grid: which column (school date) and which rows
 * (wall-clock minutes in the school time zone) a lesson occupies, and how
 * overlapping lessons share a column.
 */
import type { ScheduleLesson } from '@/api/client';
import { addDays, minutesSinceMidnight, schoolDateOf, type SchoolDate } from '@/lib/schoolTime';

export const MINUTES_PER_DAY = 24 * 60;

/** Lessons shorter than this are drawn (and collide) as if they lasted this long. */
export const MIN_BLOCK_MINUTES = 30;

/** Minutes since school-local midnight; end is exclusive and at most 24:00. */
export type Interval = { start: number; end: number };

/**
 * The school date and wall-clock interval of a lesson. A lesson that ends
 * after midnight is clipped to 24:00. If the wall clock runs backwards while
 * it lasts (clocks go back), its real length is kept instead.
 */
export function lessonInterval(
  startsAt: string,
  endsAt: string,
  timeZone: string,
): Interval & { date: SchoolDate } {
  const date = schoolDateOf(startsAt, timeZone);
  const start = minutesSinceMidnight(startsAt, timeZone);
  let end =
    schoolDateOf(endsAt, timeZone) === date
      ? minutesSinceMidnight(endsAt, timeZone)
      : MINUTES_PER_DAY;
  if (end < start) {
    end = start + Math.round((Date.parse(endsAt) - Date.parse(startsAt)) / 60_000);
  }
  return { date, start, end: Math.min(end, MINUTES_PER_DAY) };
}

/** An item of one day with its lane: lanes split the column width evenly. */
export type Placement<T> = Interval & { item: T; lane: number; lanes: number };

/**
 * Assigns lanes so that overlapping items sit side by side. Items that
 * overlap directly or through others form a cluster, and every item of a
 * cluster gets the cluster's lane count; items without overlap keep the full
 * width. Returns the items ordered by start.
 */
export function layoutDay<T>(items: readonly (Interval & { item: T })[]): Placement<T>[] {
  const sorted = [...items].sort((a, b) => a.start - b.start || b.end - a.end);
  const placed: Placement<T>[] = [];
  let cluster: Placement<T>[] = [];
  let laneEnds: number[] = [];
  let clusterEnd = -1;

  const closeCluster = () => {
    for (const p of cluster) p.lanes = laneEnds.length;
    cluster = [];
    laneEnds = [];
  };

  for (const it of sorted) {
    const visualEnd = Math.max(it.end, it.start + MIN_BLOCK_MINUTES);
    if (it.start >= clusterEnd) closeCluster();
    let lane = laneEnds.findIndex((end) => end <= it.start);
    if (lane === -1) lane = laneEnds.push(visualEnd) - 1;
    else laneEnds[lane] = visualEnd;
    clusterEnd = Math.max(clusterEnd, visualEnd);
    const p: Placement<T> = { ...it, lane, lanes: 1 };
    cluster.push(p);
    placed.push(p);
  }
  closeCluster();
  return placed;
}

/** Hours shown on the axis; endHour is the last hour line (exclusive end). */
export type TimeAxis = { startHour: number; endHour: number };

export const DEFAULT_AXIS: TimeAxis = { startHour: 8, endHour: 22 };

/**
 * From the earliest start (floored) to the latest end (ceiled), in whole
 * hours. Very short lessons count with their drawn length.
 */
export function timeAxis(intervals: readonly Interval[]): TimeAxis {
  if (intervals.length === 0) return DEFAULT_AXIS;
  const start = Math.min(...intervals.map((i) => i.start));
  const end = Math.max(...intervals.map((i) => Math.max(i.end, i.start + MIN_BLOCK_MINUTES)));
  const startHour = Math.floor(start / 60);
  const endHour = Math.min(24, Math.max(Math.ceil(end / 60), startHour + 1));
  return { startHour, endHour };
}

/** The seven school dates of the week that starts on weekStart (a Monday). */
export function weekDays(weekStart: SchoolDate): SchoolDate[] {
  return Array.from({ length: 7 }, (_, i) => addDays(weekStart, i));
}

export type WeekLayout = {
  days: { date: SchoolDate; placements: Placement<ScheduleLesson>[] }[];
  axis: TimeAxis;
  isEmpty: boolean;
};

/** Lays out the lessons that fall into the week; others are ignored. */
export function layoutWeek(
  lessons: readonly ScheduleLesson[],
  weekStart: SchoolDate,
  timeZone: string,
): WeekLayout {
  const dates = weekDays(weekStart);
  const byDate = new Map(dates.map((d) => [d, [] as (Interval & { item: ScheduleLesson })[]]));
  for (const lesson of lessons) {
    const { date, start, end } = lessonInterval(lesson.startsAt, lesson.endsAt, timeZone);
    byDate.get(date)?.push({ item: lesson, start, end });
  }
  const all = [...byDate.values()].flat();
  return {
    days: dates.map((date) => ({ date, placements: layoutDay(byDate.get(date) ?? []) })),
    axis: timeAxis(all),
    isEmpty: all.length === 0,
  };
}
