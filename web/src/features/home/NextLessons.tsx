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
export function NextLessons() {
  const { t } = useTranslation();
  const settings = useQuery(publicSettingsQuery);

  return (
    <Card role="region" aria-labelledby="next-lessons-title">
      <CardHeader>
        <CardTitle>
          <h2 id="next-lessons-title">{t('home.schedule.title')}</h2>
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
        <Button asChild variant="outline" size="sm">
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

  return (
    <ul className="divide-y rounded-lg border">
      {upcoming.map((l) => (
        <li key={l.id}>
          <Link
            to="/schedule"
            search={{ from: schoolDateOf(l.startsAt, timeZone), lesson: l.id }}
            className="flex items-baseline justify-between gap-3 px-3 py-2 text-sm hover:bg-accent focus-visible:bg-accent focus-visible:outline-none"
          >
            <span className="font-medium">{l.courseType.name}</span>
            <span className="text-muted-foreground tabular-nums">
              {formatDateTime(l.startsAt, timeZone, lang)}
            </span>
          </Link>
        </li>
      ))}
    </ul>
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
