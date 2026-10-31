import { Link } from '@tanstack/react-router';
import { ArrowRight, CircleAlert, RotateCcw, UserRound } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import type { HomeMyLessons, HomeProps, HomeStatus } from '../slots';
import { useSlot } from '../useSlot';

/**
 * Werkstatt's home page: two columns from 768px, stacked below. Left the
 * school's "Infos" (the welcome text by a round avatar, the announcements,
 * a short system status); right "My schedule", the visitor's own lessons of
 * the coming week by day (the DayHeading and LessonCard slots), or for a
 * visitor who is signed out the teaser of the next lessons.
 */
export function Home({
  schoolName,
  announcements,
  renderNextLessons,
  status,
  myLessons,
}: HomeProps) {
  const { t } = useTranslation();
  const infoId = useId();

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-light tracking-wide text-primary sm:text-4xl">
        {schoolName ?? t('home.title')}
      </h1>
      <div className="grid items-start gap-8 md:grid-cols-2">
        <section aria-labelledby={infoId} className="min-w-0 space-y-5">
          <h2 id={infoId} className="text-2xl font-light">
            {t('home.info')}
          </h2>
          <div className="flex items-start gap-4">
            <span
              aria-hidden="true"
              className="grid size-16 shrink-0 place-items-center rounded-full bg-secondary text-muted-foreground"
            >
              <UserRound className="size-8" strokeWidth={1.5} />
            </span>
            <p className="text-lg font-light text-pretty">{t('home.intro')}</p>
          </div>
          {announcements}
          <Status status={status} />
        </section>
        {myLessons.state === 'signedOut' ? (
          renderNextLessons('min-w-0')
        ) : myLessons.state === 'unknown' ? (
          // Neither column's heading yet: a signed-out visitor never sees "My schedule" flash.
          <Skeleton className="h-48 w-full" aria-hidden="true" />
        ) : (
          <MySchedule myLessons={myLessons} />
        )}
      </div>
    </div>
  );
}

function MySchedule({
  myLessons,
}: {
  myLessons: Exclude<HomeMyLessons, { state: 'signedOut' | 'unknown' }>;
}) {
  const { t } = useTranslation();
  const DayHeading = useSlot('DayHeading');
  const titleId = useId();

  return (
    <section aria-labelledby={titleId} className="min-w-0 space-y-5">
      <h2 id={titleId} className="text-2xl font-light">
        {t('home.mine.title')}
      </h2>
      {myLessons.state === 'loading' ? (
        <div role="status" className="space-y-2">
          <span className="sr-only">{t('home.mine.loading')}</span>
          <Skeleton className="h-10 w-full" aria-hidden="true" />
          <Skeleton className="h-28 w-full" aria-hidden="true" />
        </div>
      ) : myLessons.state === 'error' ? (
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{t('home.mine.loadError')}</AlertTitle>
          <AlertDescription>
            <Button variant="outline" size="sm" className="mt-2" onClick={myLessons.onRetry}>
              <RotateCcw aria-hidden="true" />
              {t('home.mine.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : myLessons.days.length === 0 ? (
        <p className="font-light text-muted-foreground">{t('home.mine.empty')}</p>
      ) : (
        <div className="space-y-5">
          {myLessons.days.map((day, i, days) => (
            <section key={day.date} aria-labelledby={day.headingId} className="space-y-2">
              <DayHeading
                id={day.headingId}
                date={day.date}
                tone={day.tone}
                prevId={days[i - 1]?.headingId}
                nextId={days[i + 1]?.headingId}
              />
              <ul className="space-y-2">
                {day.lessons.map((lesson) => (
                  <li key={lesson.id} className="flex">
                    {myLessons.renderLesson(lesson)}
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}
      <Link
        to="/schedule"
        className="inline-flex items-center gap-1.5 rounded-[3px] text-sm tracking-wide text-primary uppercase underline-offset-4 hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
      >
        {t('home.schedule.viewAll')}
        <ArrowRight aria-hidden="true" className="size-4" />
      </Link>
    </section>
  );
}

/** The backend's and the school's state in a few lines, with a retry when loading failed. */
function Status({ status }: { status: HomeStatus }) {
  const { t } = useTranslation();
  const titleId = useId();
  const { health, version, timezone, failed, onRetry } = status;

  return (
    <section aria-labelledby={titleId} className="space-y-2 border-t pt-4 text-sm">
      <h3 id={titleId} className="text-[0.78em] font-bold tracking-wide text-primary uppercase">
        {t('home.status.title')}
      </h3>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
        <dt className="text-muted-foreground">{t('home.status.backend')}</dt>
        <dd className={health === 'error' ? 'text-destructive' : undefined}>
          {health === 'pending'
            ? t('home.status.checking')
            : health === 'ok'
              ? t('home.status.ok')
              : t('home.status.error')}
        </dd>
        {version && (
          <>
            <dt className="text-muted-foreground">{t('home.status.version')}</dt>
            <dd>{version}</dd>
          </>
        )}
        {timezone && (
          <>
            <dt className="text-muted-foreground">{t('home.status.timezone')}</dt>
            <dd>{timezone}</dd>
          </>
        )}
      </dl>
      {failed && (
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{t('home.status.loadError')}</AlertTitle>
          <AlertDescription>
            <Button variant="outline" size="sm" className="mt-2" onClick={onRetry}>
              <RotateCcw aria-hidden="true" />
              {t('home.status.retry')}
            </Button>
          </AlertDescription>
        </Alert>
      )}
    </section>
  );
}
