import type { ScheduleListProps } from '../slots';
import { useSlot } from '../useSlot';

/**
 * The schedule as a feed of lesson cards grouped by school day, each day a
 * region under its sticky heading (the DayHeading slot); `empty` without
 * days.
 */
export function ScheduleList({ days, empty, renderLesson }: ScheduleListProps) {
  const DayHeading = useSlot('DayHeading');
  if (days.length === 0) return empty;

  return (
    <div className="space-y-6">
      {days.map((day, i) => (
        <section key={day.date} aria-labelledby={day.headingId} className="space-y-3">
          <DayHeading
            id={day.headingId}
            date={day.date}
            tone={day.tone}
            prevId={days[i - 1]?.headingId}
            nextId={days[i + 1]?.headingId}
          />
          <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {day.lessons.map((lesson) => (
              <li key={lesson.id} className="flex">
                {renderLesson(lesson)}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
