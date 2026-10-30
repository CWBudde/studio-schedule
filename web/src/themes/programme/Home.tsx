import { CircleAlert, RotateCcw } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
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
import { Ornament } from '@/components/layout/Ornament';
import { Skeleton } from '@/components/ui/skeleton';
import type { HomeProps, HomeStatus } from '../slots';

/** The home page laid out like the cover of a programme, with the status card. */
export function Home({ schoolName, announcements, renderNextLessons, status }: HomeProps) {
  const { t } = useTranslation();

  return (
    <div className="space-y-12">
      {/* Laid out like the cover of a programme. */}
      <section aria-labelledby="home-title" className="mx-auto max-w-2xl pt-6 text-center sm:pt-10">
        <p className="eyebrow text-primary">{t('home.eyebrow')}</p>
        <h1
          id="home-title"
          className="mt-4 font-heading text-4xl leading-[1.05] font-medium tracking-tight text-balance sm:text-6xl"
        >
          {schoolName ?? t('home.title')}
        </h1>
        <Ornament className="mx-auto mt-7 max-w-56" />
        <p className="mx-auto mt-6 max-w-prose text-pretty text-muted-foreground sm:text-lg">
          {t('home.intro')}
        </p>
      </section>

      {announcements}

      <div className="grid items-start gap-6 md:grid-cols-5">
        {renderNextLessons('md:col-span-3')}
        <StatusCard className="md:col-span-2" schoolName={schoolName} status={status} />
      </div>
    </div>
  );
}

function StatusCard({
  className,
  schoolName,
  status,
}: {
  className?: string;
  schoolName?: string;
  status: HomeStatus;
}) {
  const { t } = useTranslation();
  const { health, version, timezone, settingsPending, failed, onRetry } = status;

  return (
    <Card role="region" aria-labelledby="status-title" className={className}>
      <CardHeader>
        <CardTitle>
          <h2 id="status-title" className="text-xl">
            {t('home.status.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('home.status.description')}</CardDescription>
        <CardAction>
          {health === 'pending' ? (
            <Badge variant="outline">{t('home.status.checking')}</Badge>
          ) : health === 'ok' ? (
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
          {version && (
            <Row label={t('home.status.version')} loading={false}>
              {version}
            </Row>
          )}
          <Row label={t('home.status.school')} loading={settingsPending}>
            {schoolName}
          </Row>
          <Row label={t('home.status.timezone')} loading={settingsPending}>
            {timezone}
          </Row>
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
