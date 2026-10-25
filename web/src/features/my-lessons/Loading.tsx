import { useTranslation } from 'react-i18next';
import { Skeleton } from '@/components/ui/skeleton';

/** The skeleton of a part of "My lessons" while its lessons load. */
export function Loading() {
  const { t } = useTranslation('myLessons');
  return (
    <div role="status" className="space-y-3">
      <span className="sr-only">{t('loading')}</span>
      <Skeleton className="h-24 w-full" aria-hidden="true" />
      <Skeleton className="h-24 w-full" aria-hidden="true" />
    </div>
  );
}
