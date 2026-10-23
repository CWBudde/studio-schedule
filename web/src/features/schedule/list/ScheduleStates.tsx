import { CircleAlert, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';

/** Placeholder while the schedule loads: two days of skeleton cards. */
export function ScheduleLoading() {
  const { t } = useTranslation('schedule');
  return (
    <div role="status" className="space-y-6">
      <span className="sr-only">{t('list.loading')}</span>
      {[0, 1].map((day) => (
        <div key={day} className="space-y-3" aria-hidden="true">
          <Skeleton className="h-6 w-48" />
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
            {[0, 1, 2].map((card) => (
              <Skeleton key={card} className="h-32 rounded-xl" />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

/** The schedule could not be loaded; offers to try again. */
export function ScheduleError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation('schedule');
  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{t('list.error.title')}</AlertTitle>
      <AlertDescription>
        <p>{t('list.error.description')}</p>
        <Button variant="outline" size="sm" className="mt-2" onClick={onRetry}>
          <RotateCcw aria-hidden="true" />
          {t('list.error.retry')}
        </Button>
      </AlertDescription>
    </Alert>
  );
}
