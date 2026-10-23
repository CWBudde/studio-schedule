import { CircleAlert, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

/** A failed load: `message` (default: the data could not be loaded) and a retry button. */
export function LoadError({ message, onRetry }: { message?: string; onRetry?: () => void }) {
  const { t } = useTranslation('admin');
  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{message ?? t('table.loadError')}</AlertTitle>
      {onRetry && (
        <AlertDescription>
          <Button variant="outline" size="sm" className="mt-2" onClick={onRetry}>
            <RotateCcw aria-hidden="true" />
            {t('table.retry')}
          </Button>
        </AlertDescription>
      )}
    </Alert>
  );
}
