import { Clock, MapPin, UserRound } from 'lucide-react';
import { useId, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import {
  endTime,
  groupByWeekday,
  type WeekdayGroup,
  type WeeklyClass,
  weekdayName,
} from './timetable';

/** Names of the catalogue entries a class refers to, by ID. */
export type Catalogue = {
  courseTypes: Map<string, string>;
  rooms: Map<string, { name: string; locationId: string }>;
  locations: Map<string, string>;
  staff: Map<string, string>;
};

/**
 * The weekly classes of a term grouped by weekday, Monday first, each day
 * under a sticky heading. Days without classes are left out.
 */
export function WeekList({ classes, catalogue }: { classes: WeeklyClass[]; catalogue: Catalogue }) {
  const { t, i18n } = useTranslation('timetable');
  const lang = i18n.resolvedLanguage ?? 'en';
  const days = useMemo(() => groupByWeekday(classes), [classes]);

  return (
    <div role="group" aria-label={t('week.label')} className="space-y-6">
      {days.map((day) => (
        <DaySection key={day.weekday} day={day} lang={lang} catalogue={catalogue} />
      ))}
    </div>
  );
}

function DaySection({
  day,
  lang,
  catalogue,
}: {
  day: WeekdayGroup;
  lang: string;
  catalogue: Catalogue;
}) {
  const headingId = useId();
  const name = weekdayName(day.weekday, lang);
  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <h2
        id={headingId}
        className="sticky top-0 z-10 -mx-1 bg-background/90 px-1 py-2 font-heading text-lg font-semibold tracking-tight backdrop-blur supports-[backdrop-filter]:bg-background/75"
      >
        {name}
      </h2>
      <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {day.classes.map((c) => (
          <li key={c.id} className="flex">
            <ClassCard weeklyClass={c} dayName={name} catalogue={catalogue} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function ClassCard({
  weeklyClass: c,
  dayName,
  catalogue,
}: {
  weeklyClass: WeeklyClass;
  dayName: string;
  catalogue: Catalogue;
}) {
  const { t } = useTranslation('timetable');
  const { t: ts } = useTranslation('schedule');
  const unknown = t('class.unknown');
  const courseType = catalogue.courseTypes.get(c.courseTypeId) ?? unknown;
  const room = catalogue.rooms.get(c.roomId);
  const location = room && catalogue.locations.get(room.locationId);
  const place = room ? [room.name, location].filter(Boolean).join(' · ') : unknown;
  const instructor = catalogue.staff.get(c.instructorId) ?? unknown;
  const accompanist = c.accompanistId && catalogue.staff.get(c.accompanistId);
  const time = `${c.startTime}–${endTime(c.startTime, c.durationMinutes)}`;

  return (
    <article
      aria-label={t('class.cardLabel', { courseType, day: dayName, time })}
      className="flex w-full flex-col gap-2 rounded-xl bg-card p-4 text-sm text-card-foreground ring-1 ring-foreground/10"
    >
      <span className="inline-flex items-center gap-1.5 font-medium tabular-nums">
        <Clock className="size-4 text-muted-foreground" aria-hidden="true" />
        <span>{time}</span>
      </span>

      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-heading text-base font-semibold">{courseType}</h3>
        <Badge variant="secondary">{ts(`level.${c.level}`)}</Badge>
      </div>

      <div className="flex flex-col gap-1 text-muted-foreground">
        <p className="inline-flex items-start gap-1.5">
          <MapPin className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span className="sr-only">{t('class.place')}: </span>
          <span>{place}</span>
        </p>
        <p className="inline-flex items-start gap-1.5">
          <UserRound className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
          <span className="sr-only">{t('class.instructor')}: </span>
          <span>
            <span>{instructor}</span>
            {accompanist && (
              <>
                {' · '}
                <span>{t('class.accompaniedBy', { name: accompanist })}</span>
              </>
            )}
          </span>
        </p>
      </div>
    </article>
  );
}
