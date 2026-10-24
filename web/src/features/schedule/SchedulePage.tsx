import { useQuery } from '@tanstack/react-query';
import { CalendarDays, ChevronLeft, ChevronRight, List } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { Schedule } from '@/api/client';
import { publicSettingsQuery, scheduleQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import {
  addDays,
  formatDate,
  formatDateTime,
  formatDayHeading,
  schoolDateOf,
  startOfWeek,
  todayIn,
} from '@/lib/schoolTime';
import { NewLesson } from './admin/NewLesson';
import { ReleaseCountdown } from './countdown/ReleaseCountdown';
import { LessonSheet } from './detail/LessonSheet';
import { ScheduleFilters } from './filters/ScheduleFilters';
import { ScheduleList } from './list/ScheduleList';
import { ScheduleError, ScheduleLoading } from './list/ScheduleStates';
import { useScheduleSearch } from './useScheduleSearch';
import { WeekGrid } from './week/WeekGrid';

/** Days the list view shows at once (the API allows up to 42). */
const listDays = 14;

export function SchedulePage() {
  const { t } = useTranslation('schedule');
  const settings = useQuery(publicSettingsQuery);

  return (
    <div className="space-y-6">
      <h1 className="font-heading text-4xl font-medium tracking-tight">{t('title')}</h1>
      {settings.data ? (
        <ScheduleView timeZone={settings.data.timezone} />
      ) : settings.isError ? (
        <ScheduleError onRetry={() => void settings.refetch()} />
      ) : (
        <ScheduleLoading />
      )}
      <LessonSheet />
    </div>
  );
}

function ScheduleView({ timeZone }: { timeZone: string }) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const { search, update } = useScheduleSearch();

  const today = todayIn(timeZone);
  const view = search.view ?? 'list';
  const from = search.from ?? today;
  const start = view === 'week' ? startOfWeek(from) : from;
  const end = addDays(start, view === 'week' ? 7 : listDays);

  const schedule = useQuery(
    scheduleQuery({
      from: start,
      to: end,
      location: search.location,
      instructor: search.instructor,
      courseType: search.courseType,
      level: search.level,
    }),
  );
  // The release times do not depend on the range or the filters: a small
  // query of its own keeps the countdown steady while the view changes.
  const release = useQuery(scheduleQuery({ from: today, to: addDays(today, 1) }));

  return (
    <>
      {release.data && (
        <ReleaseCountdown
          nextReleaseAt={release.data.nextReleaseAt}
          now={release.data.now}
          receivedAt={release.dataUpdatedAt}
          timeZone={release.data.timezone}
        />
      )}

      <ScheduleFilters />

      <div className="flex flex-wrap items-center justify-between gap-3">
        <ToggleGroup
          type="single"
          variant="outline"
          spacing={0}
          value={view}
          onValueChange={(value) => {
            if (value) void update({ view: value === 'week' ? 'week' : undefined });
          }}
          aria-label={t('view.label')}
        >
          <ToggleGroupItem value="list">
            <List aria-hidden="true" />
            {t('view.list')}
          </ToggleGroupItem>
          <ToggleGroupItem value="week">
            <CalendarDays aria-hidden="true" />
            {t('view.week')}
          </ToggleGroupItem>
        </ToggleGroup>

        {view === 'list' && (
          <nav aria-label={t('page.rangeNav')} className="flex items-center gap-1">
            <Button
              variant="outline"
              size="icon"
              aria-label={t('page.previous', { count: listDays })}
              onClick={() => void update({ from: addDays(from, -listDays) })}
            >
              <ChevronLeft aria-hidden="true" />
            </Button>
            <span className="min-w-36 text-center text-sm font-medium tabular-nums">
              {t('page.range', {
                from: formatDate(start, lang, { day: 'numeric', month: 'short' }),
                to: formatDate(addDays(end, -1), lang, { day: 'numeric', month: 'short' }),
              })}
            </span>
            <Button
              variant="outline"
              size="icon"
              aria-label={t('page.next', { count: listDays })}
              onClick={() => void update({ from: addDays(from, listDays) })}
            >
              <ChevronRight aria-hidden="true" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={from === today}
              onClick={() => void update({ from: undefined })}
            >
              {t('page.today')}
            </Button>
          </nav>
        )}

        <NewLesson />
      </div>

      {schedule.data ? (
        <>
          {view === 'week' ? (
            <WeekGrid schedule={schedule.data} weekStart={start} today={today} />
          ) : (
            <ScheduleList schedule={schedule.data} receivedAt={schedule.dataUpdatedAt} />
          )}
          <ReleaseNote schedule={schedule.data} lastDay={addDays(end, -1)} />
        </>
      ) : schedule.isError ? (
        <ScheduleError onRetry={() => void schedule.refetch()} />
      ) : (
        <ScheduleLoading />
      )}
    </>
  );
}

/**
 * Says what of the timetable the viewer sees (ADR 0003). Guests: up to
 * which day regular classes are released, when the shown range goes beyond
 * that day, so the end of the list does not look like the end of the
 * timetable. Members with early access (visibleUntil after releasedUntil):
 * up to which day they see classes, in the same case. Staff (no
 * visibleUntil): that they see every lesson, always.
 */
function ReleaseNote({ schedule, lastDay }: { schedule: Schedule; lastDay: string }) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  // releasedUntil and visibleUntil are the school midnight after the last day.
  const lastDayBefore = (instant: string) =>
    schoolDateOf(Date.parse(instant) - 1, schedule.timezone);
  const lastReleased = lastDayBefore(schedule.releasedUntil);
  const day = formatDayHeading(lastReleased, lang);

  let note: string;
  if (schedule.visibleUntil === undefined) {
    note = t('page.staffView', { day });
  } else if (lastReleased >= lastDay) {
    return null;
  } else if (Date.parse(schedule.visibleUntil) > Date.parse(schedule.releasedUntil)) {
    note = t('page.earlyAccess', {
      visible: formatDayHeading(lastDayBefore(schedule.visibleUntil), lang),
      day,
    });
  } else {
    note = t('page.releasedThrough', {
      day,
      release: formatDateTime(schedule.nextReleaseAt, schedule.timezone, lang),
    });
  }

  return <p className="text-sm text-muted-foreground">{note}</p>;
}
