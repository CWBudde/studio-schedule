import { useQuery } from '@tanstack/react-query';
import { CalendarClock, CircleAlert, RotateCcw } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { healthQuery, publicSettingsQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';

export function HomePage() {
  const { t } = useTranslation();
  const settings = useQuery(publicSettingsQuery);

  return (
    <div className="space-y-8">
      <section aria-labelledby="home-title" className="space-y-2">
        <h1 id="home-title" className="font-heading text-3xl font-semibold tracking-tight">
          {settings.data?.schoolName ?? t('home.title')}
        </h1>
        <p className="max-w-prose text-muted-foreground">{t('home.intro')}</p>
      </section>

      <div className="grid gap-6 md:grid-cols-2">
        <StatusCard />
        <SchedulePlaceholder />
      </div>
    </div>
  );
}

function StatusCard() {
  const { t } = useTranslation();
  const health = useQuery(healthQuery);
  const settings = useQuery(publicSettingsQuery);
  const isOk = health.data?.status === 'ok';
  const failed = health.isError || settings.isError;

  return (
    <Card aria-labelledby="status-title">
      <CardHeader>
        <CardTitle>
          <h2 id="status-title">{t('home.status.title')}</h2>
        </CardTitle>
        <CardDescription>{t('home.status.description')}</CardDescription>
        <CardAction>
          {health.isPending ? (
            <Badge variant="outline">{t('home.status.checking')}</Badge>
          ) : isOk ? (
            <Badge className="bg-emerald-600 text-white dark:bg-emerald-500 dark:text-emerald-950">
              <span className="sr-only">{t('home.status.backend')}: </span>
              {t('home.status.ok')}
            </Badge>
          ) : (
            <Badge variant="destructive">
              <span className="sr-only">{t('home.status.backend')}: </span>
              {t('home.status.error')}
            </Badge>
          )}
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-4">
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 text-sm">
          <Row label={t('home.status.version')} loading={health.isPending}>
            {health.data?.version}
          </Row>
          <Row label={t('home.status.school')} loading={settings.isPending}>
            {settings.data?.schoolName}
          </Row>
          <Row label={t('home.status.timezone')} loading={settings.isPending}>
            {settings.data?.timezone}
          </Row>
        </dl>

        {failed && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('home.status.loadError')}</AlertTitle>
            <AlertDescription>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() => {
                  void health.refetch();
                  void settings.refetch();
                }}
              >
                <RotateCcw aria-hidden="true" />
                {t('home.status.retry')}
              </Button>
            </AlertDescription>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}

function Row({
  label,
  loading,
  children,
}: {
  label: string;
  loading: boolean;
  children: ReactNode;
}) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 truncate font-medium">
        {loading ? <Skeleton className="h-4 w-28" /> : (children ?? '—')}
      </dd>
    </>
  );
}

function SchedulePlaceholder() {
  const { t } = useTranslation();
  return (
    <Card aria-labelledby="schedule-title" className="border-dashed">
      <CardHeader>
        <CardTitle>
          <h2 id="schedule-title">{t('home.schedule.title')}</h2>
        </CardTitle>
        <CardDescription>{t('home.schedule.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col items-center gap-3 rounded-lg bg-accent px-4 py-8 text-center text-accent-foreground">
          <CalendarClock className="size-8" aria-hidden="true" />
          <p className="font-medium">{t('home.schedule.comingSoon')}</p>
        </div>
      </CardContent>
    </Card>
  );
}
