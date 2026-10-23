import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { ArrowRight, CircleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { publicSettingsQuery, scheduleQuery } from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { addDays, formatDateTime, schoolDateOf, todayIn } from '@/lib/schoolTime';

/** How many upcoming lessons the home page shows. */
const shown = 3;

/** Teaser for the schedule: the next lessons of the coming week. */
export function NextLessons({ className }: { className?: string }) {
  const { t } = useTranslation();
  const settings = useQuery(publicSettingsQuery);

  return (
    <Card role="region" aria-labelledby="next-lessons-title" className={className}>
      <CardHeader>
        <CardTitle>
          <h2 id="next-lessons-title" className="text-2xl">
            {t('home.schedule.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('home.schedule.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {settings.data ? (
          <Upcoming timeZone={settings.data.timezone} />
        ) : settings.isError ? (
          <LoadError />
        ) : (
          <Loading />
        )}
        <Button asChild size="sm">
          <Link to="/schedule">
            {t('home.schedule.viewAll')}
            <ArrowRight aria-hidden="true" />
          </Link>
        </Button>
      </CardContent>
    </Card>
  );
}

function Upcoming({ timeZone }: { timeZone: string }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.resolvedLanguage ?? 'en';
  const today = todayIn(timeZone);
  const schedule = useQuery(scheduleQuery({ from: today, to: addDays(today, 7) }));

  if (schedule.isError) return <LoadError />;
  if (!schedule.data) return <Loading />;

  const now = Date.parse(schedule.data.now);
  const upcoming = schedule.data.lessons
    .filter((l) => l.status !== 'canceled' && Date.parse(l.endsAt) > now)
    .slice(0, shown);
  if (upcoming.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('home.schedule.empty')}</p>;
  }

  // A programme's running order: the piece, a dotted leader, then when it starts.
  return (
    <ol className="-mx-2">
      {upcoming.map((l) => (
        <li key={l.id}>
          <Link
            to="/schedule"
            search={{ from: schoolDateOf(l.startsAt, timeZone), lesson: l.id }}
            className="group flex items-baseline gap-3 rounded-md px-2 py-2.5 transition-colors hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
          >
            <span className="font-heading text-lg font-medium group-hover:text-accent-foreground">
              {l.courseType.name}
            </span>
            <span
              aria-hidden="true"
              className="min-w-6 flex-1 border-b border-dotted border-foreground/30"
            />
            <span className="eyebrow whitespace-nowrap text-muted-foreground tabular-nums">
              {formatDateTime(l.startsAt, timeZone, lang)}
            </span>
          </Link>
        </li>
      ))}
    </ol>
  );
}

function Loading() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-full" />
    </div>
  );
}

function LoadError() {
  const { t } = useTranslation();
  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{t('home.schedule.loadError')}</AlertTitle>
    </Alert>
  );
}
