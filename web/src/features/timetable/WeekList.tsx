import { Clock, MapPin, UserRound } from 'lucide-react';
import { type ReactNode, useId, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { formatAudience } from '@/lib/audience';
import {
  endTime,
  groupByWeekday,
  type WeekdayGroup,
  type WeeklyClass,
  weekdayName,
} from './timetable';

/** Names of the catalogue entries a class refers to, by ID. Staff come with the class. */
export type Catalogue = {
  courseTypes: Map<string, string>;
  rooms: Map<string, { name: string; locationId: string }>;
  locations: Map<string, string>;
};

/** Extras of a class card, e.g. the office's buttons; the public list has none. */
export type CardExtras = {
  /** Badges next to whom the class is for, such as "Retired". */
  badges?: (weeklyClass: WeeklyClass) => ReactNode;
  /** Buttons at the foot of the card. */
  actions?: (weeklyClass: WeeklyClass) => ReactNode;
};

/**
 * The weekly classes of a term grouped by weekday, Monday first, each day
 * under a sticky heading. Days without classes are left out.
 */
export function WeekList({
  classes,
  catalogue,
  badges,
  actions,
}: { classes: WeeklyClass[]; catalogue: Catalogue } & CardExtras) {
  const { t, i18n } = useTranslation('timetable');
  const lang = i18n.resolvedLanguage ?? 'en';
  const days = useMemo(() => groupByWeekday(classes), [classes]);

  return (
    <div role="group" aria-label={t('week.label')} className="space-y-6">
      {days.map((day) => (
        <DaySection
          key={day.weekday}
          day={day}
          lang={lang}
          catalogue={catalogue}
          badges={badges}
          actions={actions}
        />
      ))}
    </div>
  );
}

function DaySection({
  day,
  lang,
  catalogue,
  badges,
  actions,
}: {
  day: WeekdayGroup;
  lang: string;
  catalogue: Catalogue;
} & CardExtras) {
  const headingId = useId();
  const name = weekdayName(day.weekday, lang);
  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <h2
        id={headingId}
        className="sticky top-0 z-10 -mx-1 flex items-center gap-4 bg-background/90 px-1 py-2 font-heading text-2xl font-medium tracking-tight italic backdrop-blur after:h-px after:flex-1 after:bg-border supports-[backdrop-filter]:bg-background/75"
      >
        {name}
      </h2>
      <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {day.classes.map((c) => (
          <li key={c.id} className="flex">
            <ClassCard
              weeklyClass={c}
              dayName={name}
              catalogue={catalogue}
              badges={badges}
              actions={actions}
            />
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
  badges,
  actions,
}: {
  weeklyClass: WeeklyClass;
  dayName: string;
  catalogue: Catalogue;
} & CardExtras) {
  const { t } = useTranslation('timetable');
  const { t: ts } = useTranslation('schedule');
  const unknown = t('class.unknown');
  const courseType = catalogue.courseTypes.get(c.courseTypeId) ?? unknown;
  const room = catalogue.rooms.get(c.roomId);
  const location = room && catalogue.locations.get(room.locationId);
  const place = room ? [room.name, location].filter(Boolean).join(' · ') : unknown;
  const instructor = c.instructor.displayName;
  const accompanist = c.accompanist?.displayName;
  const time = `${c.startTime}–${endTime(c.startTime, c.durationMinutes)}`;
  const audience = formatAudience(c, ts, 'long');

  return (
    <article
      aria-label={t('class.cardLabel', { courseType, day: dayName, time })}
      className="flex w-full flex-col gap-2 rounded-lg bg-card p-4 pl-5 text-sm text-card-foreground shadow-[inset_2px_0_0_0_var(--color-primary)] ring-1 ring-foreground/10"
    >
      <span className="inline-flex items-center gap-1.5 font-medium text-primary tabular-nums">
        <Clock className="size-4" aria-hidden="true" />
        <span>{time}</span>
      </span>

      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-heading text-lg leading-snug font-medium">{courseType}</h3>
        {audience && <Badge variant="secondary">{audience}</Badge>}
        {badges?.(c)}
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
      {actions && <div className="mt-auto flex flex-wrap justify-end gap-2 pt-2">{actions(c)}</div>}
    </article>
  );
}
