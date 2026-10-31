import type { ScheduleLesson } from '@/api/client';

/** One room's column of a day: its lessons by start time. */
export interface RoomColumn {
  /** The room's id. */
  key: string;
  room: ScheduleLesson['room'];
  location: ScheduleLesson['location'];
  /** The room's name, with its location's when the lessons span more than one. */
  label: string;
  lessons: ScheduleLesson[];
}

/**
 * The rooms that `lessons` use, one column each, ordered by location name,
 * then room name (in `lang`), then id; a column's lessons by start time,
 * keeping their order on ties.
 */
export function roomColumns(lessons: ScheduleLesson[], lang: string): RoomColumn[] {
  const byRoom = new Map<string, RoomColumn>();
  for (const lesson of lessons) {
    const column = byRoom.get(lesson.room.id);
    if (column) column.lessons.push(lesson);
    else
      byRoom.set(lesson.room.id, {
        key: lesson.room.id,
        room: lesson.room,
        location: lesson.location,
        label: lesson.room.name,
        lessons: [lesson],
      });
  }
  const columns = [...byRoom.values()];
  const locations = new Set(columns.map((c) => c.location.id));
  for (const column of columns) {
    if (locations.size > 1) column.label = `${column.room.name} · ${column.location.name}`;
    column.lessons.sort((a, b) => Date.parse(a.startsAt) - Date.parse(b.startsAt));
  }
  return columns.sort(
    (a, b) =>
      a.location.name.localeCompare(b.location.name, lang) ||
      a.room.name.localeCompare(b.room.name, lang) ||
      (a.key < b.key ? -1 : a.key > b.key ? 1 : 0),
  );
}
