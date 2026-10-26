import { RotateCcw, TriangleAlert } from 'lucide-react';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card';

/**
 * A code chunk that failed to load (Chrome, Firefox, Safari): an update
 * replaced it or the connection dropped. Retrying the render loads nothing
 * new; only a reload does.
 */
const CHUNK_LOAD_ERROR =
  /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed/i;

function isChunkLoadError(error: unknown): boolean {
  return error instanceof Error && CHUNK_LOAD_ERROR.test(error.message);
}

/**
 * Fallback for a route that threw while rendering (the router's
 * defaultErrorComponent): a translated card inside the app's layout instead
 * of the router's built-in English one in place of the whole app. Retry
 * re-renders the route; reload starts over, and is the only choice when a
 * code chunk failed to load. The error itself goes to the console only.
 */
export function RouteError({ error, reset }: { error: unknown; reset: () => void }) {
  const { t } = useTranslation();
  const chunk = isChunkLoadError(error);

  useEffect(() => {
    console.error('route render failed', error);
  }, [error]);

  return (
    <div className="mx-auto w-full max-w-md py-12">
      <Card role="alert">
        <CardHeader>
          <div className="mb-2 flex size-10 items-center justify-center rounded-full bg-destructive/10 text-destructive">
            <TriangleAlert className="size-5" aria-hidden="true" />
          </div>
          <CardTitle>
            <h1>{t('routeError.title')}</h1>
          </CardTitle>
          <CardDescription>
            {chunk ? t('routeError.outdated') : t('routeError.description')}
          </CardDescription>
        </CardHeader>
        <CardFooter className="flex-wrap gap-2">
          {chunk ? null : <Button onClick={reset}>{t('routeError.retry')}</Button>}
          <Button variant={chunk ? 'default' : 'outline'} onClick={() => window.location.reload()}>
            <RotateCcw aria-hidden="true" />
            {t('routeError.reload')}
          </Button>
        </CardFooter>
      </Card>
    </div>
  );
}
