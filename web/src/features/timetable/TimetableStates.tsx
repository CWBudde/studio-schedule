import { CalendarX2, CircleAlert, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';

/** Placeholder while the classes load: two weekdays of skeleton cards. */
export function TimetableLoading() {
  const { t } = useTranslation('timetable');
  return (
    <div role="status" className="space-y-6">
      <span className="sr-only">{t('loading')}</span>
      {[0, 1].map((day) => (
        <div key={day} className="space-y-3" aria-hidden="true">
          <Skeleton className="h-6 w-32" />
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

/** The classes could not be loaded; offers to try again. */
export function TimetableError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation('timetable');
  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{t('error.title')}</AlertTitle>
      <AlertDescription>
        <p>{t('error.description')}</p>
        <Button variant="outline" size="sm" className="mt-2" onClick={onRetry}>
          <RotateCcw aria-hidden="true" />
          {t('error.retry')}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

/** Nothing to show: no term at all, or no classes in the term. */
export function TimetableEmpty({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-10 text-center">
      <CalendarX2 className="size-8 text-muted-foreground" aria-hidden="true" />
      <p className="font-heading text-lg font-semibold">{title}</p>
      <p className="max-w-prose text-muted-foreground">{description}</p>
    </div>
  );
}
