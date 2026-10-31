import { type CSSProperties, useId, useSyncExternalStore } from 'react';
import { useTranslation } from 'react-i18next';
import type { ScheduleDay, ScheduleListProps } from '../slots';
import { useSlot } from '../useSlot';
import { roomColumns } from './roomColumns';

/** The width from which a day's lessons stand in room columns. */
const WIDE = '(min-width: 768px)';

function subscribe(onChange: () => void) {
  const media = window.matchMedia(WIDE);
  media.addEventListener('change', onChange);
  return () => media.removeEventListener('change', onChange);
}

/** Whether the room columns fit; follows rotations and resizes. */
function useWide(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => window.matchMedia(WIDE).matches,
    () => false,
  );
}

/**
 * The schedule by school day, each day a region under its heading (the
 * DayHeading slot). From 768px a day's lessons stand in one column per room
 * it uses, under a grey row naming the rooms; below, one column of cards.
 * Columns never get narrower than 13rem: with many rooms the day scrolls
 * sideways instead. `empty` without days.
 */
export function ScheduleList({ days, empty, renderLesson }: ScheduleListProps) {
  const DayHeading = useSlot('DayHeading');
  const wide = useWide();
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
          {wide ? (
            <RoomColumns day={day} renderLesson={renderLesson} />
          ) : (
            <ul className="grid gap-3">
              {day.lessons.map((lesson) => (
                <li key={lesson.id} className="flex">
                  {renderLesson(lesson)}
                </li>
              ))}
            </ul>
          )}
        </section>
      ))}
    </div>
  );
}

/** A day's lessons in room columns, under the grey row of their rooms' names. */
function RoomColumns({
  day,
  renderLesson,
}: {
  day: ScheduleDay;
  renderLesson: ScheduleListProps['renderLesson'];
}) {
  const { i18n } = useTranslation();
  const idPrefix = useId();
  const columns = roomColumns(day.lessons, i18n.resolvedLanguage ?? 'en');

  return (
    <div className="overflow-x-auto">
      <div
        className="grid grid-cols-(--room-columns)"
        style={
          { '--room-columns': `repeat(${columns.length}, minmax(13rem, 1fr))` } as CSSProperties
        }
      >
        {columns.map((column, i) => {
          const headerId = `${idPrefix}${i}`;
          return (
            <div key={column.key} className="flex min-w-0 flex-col gap-3">
              {/* No gap between the columns' header cells: the grey row reads as one band. */}
              <p
                id={headerId}
                className="truncate bg-secondary px-3 py-2.5 text-[0.78rem] font-bold tracking-wide text-primary uppercase"
              >
                {column.label}
              </p>
              <ul
                aria-labelledby={headerId}
                className="grid content-start gap-3 px-1.5 [div:first-child>&]:pl-0 [div:last-child>&]:pr-0"
              >
                {column.lessons.map((lesson) => (
                  <li key={lesson.id} className="flex">
                    {renderLesson(lesson)}
                  </li>
                ))}
              </ul>
            </div>
          );
        })}
      </div>
    </div>
  );
}
