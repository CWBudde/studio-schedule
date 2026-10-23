import { useQueryClient } from '@tanstack/react-query';
import { CalendarClock } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { scheduleQueryKey } from '@/api/queries';
import { formatDateTime } from '@/lib/schoolTime';

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** How often to refetch the schedule while a due release has not arrived yet. */
const RETRY = 30 * SECOND;

/**
 * Time left until the next release of the schedule, as the server sees it.
 *
 * The props come from the schedule response. The server computes the release
 * (ADR 0003); the client only counts down. It corrects its own clock by the
 * difference between the server's `now` and the client time the response
 * arrived (`receivedAt`, e.g. the query's dataUpdatedAt), so neither a wrong
 * browser clock nor cached data shifts the countdown. At zero it refetches
 * the schedule, and again every 30 seconds until a schedule with a new
 * release arrives, and says so meanwhile.
 */
export function ReleaseCountdown({
  nextReleaseAt,
  now,
  receivedAt,
  timeZone,
}: {
  nextReleaseAt: string;
  now: string;
  receivedAt: number;
  timeZone: string;
}) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const queryClient = useQueryClient();

  // Until the first tick, show the time left at the moment of the response.
  const key = `${now}|${nextReleaseAt}`;
  const [state, setState] = useState({ key, remaining: remainingAtResponse(nextReleaseAt, now) });
  if (state.key !== key) {
    setState({ key, remaining: remainingAtResponse(nextReleaseAt, now) });
  }
  const lastRefetch = useRef(-Infinity);

  useEffect(() => {
    const release = Date.parse(nextReleaseAt);
    // Server time minus client time, as of the response.
    const offset = Date.parse(now) - receivedAt;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const tick = () => {
      const remaining = release - (Date.now() + offset);
      setState({ key, remaining });
      if (remaining > 0) {
        timer = setTimeout(tick, untilNextChange(remaining));
        return;
      }
      // Due: refetch until the schedule brings the next release (new props).
      if (Date.now() - lastRefetch.current >= RETRY) {
        lastRefetch.current = Date.now();
        void queryClient.invalidateQueries({ queryKey: scheduleQueryKey });
      }
      timer = setTimeout(tick, RETRY);
    };
    timer = setTimeout(tick, 0);
    return () => clearTimeout(timer);
  }, [key, nextReleaseAt, now, receivedAt, queryClient]);

  if (state.remaining <= 0) {
    return (
      <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground">
        <CalendarClock className="size-4 shrink-0" aria-hidden="true" />
        {t('countdown.releasing')}
      </p>
    );
  }

  return (
    <div className="flex items-start gap-2 text-sm text-muted-foreground">
      <CalendarClock className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <div>
        <p role="timer" aria-live="off" className="font-medium text-foreground tabular-nums">
          {t('countdown.in', {
            duration: formatRemaining(state.remaining, lang, (unit, count) =>
              t(`countdown.${unit}`, { count }),
            ),
          })}
        </p>
        <p>{t('countdown.at', { at: formatDateTime(nextReleaseAt, timeZone, lang) })}</p>
      </div>
    </div>
  );
}

function remainingAtResponse(nextReleaseAt: string, now: string): number {
  return Date.parse(nextReleaseAt) - Date.parse(now);
}

/** Whether the remaining time is shown as a clock (m:ss), i.e. under an hour. */
function showsClock(remaining: number): boolean {
  return Math.ceil(remaining / SECOND) * SECOND < HOUR;
}

/**
 * Milliseconds until the shown text changes: every second in the last hour,
 * otherwise every minute (and at the switch to the clock).
 */
function untilNextChange(remaining: number): number {
  if (showsClock(remaining)) return remaining % SECOND || SECOND;
  const nextMinute = remaining % MINUTE || MINUTE;
  return Math.min(nextMinute, remaining - (HOUR - SECOND));
}

type Unit = 'days' | 'hours' | 'minutes';

function formatRemaining(
  remaining: number,
  lang: string,
  unit: (unit: Unit, count: number) => string,
): string {
  if (showsClock(remaining)) {
    const seconds = Math.ceil(remaining / SECOND);
    return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
  }
  const minutes = Math.ceil(remaining / MINUTE);
  const days = Math.floor(minutes / (DAY / MINUTE));
  const hours = Math.floor((minutes % (DAY / MINUTE)) / 60);
  const parts =
    days > 0
      ? [unit('days', days), hours > 0 && unit('hours', hours)]
      : [unit('hours', hours), minutes % 60 > 0 && unit('minutes', minutes % 60)];
  return new Intl.ListFormat(lang, { type: 'conjunction' }).format(
    parts.filter((p): p is string => typeof p === 'string'),
  );
}
