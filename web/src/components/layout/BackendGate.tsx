import type { ReactNode } from 'react';
import { AppWindow, Loader2, RotateCcw, ServerCrash } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useBackendStatus } from '@/api/backendStatus';
import { demoControls } from '@/api/transport';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { ResetDemoDialog } from './ResetDemo';

/** Renders children only once the backend is ready (instant in HTTP mode). */
export function BackendGate({ children }: { children: ReactNode }) {
  const status = useBackendStatus();

  if (status.state === 'loading') return <BackendLoading />;
  if (status.state === 'error') return <BackendCrashed message={status.message} />;
  if (status.state === 'elsewhere') return <DemoElsewhere />;
  return children;
}

function BackendLoading() {
  const { t } = useTranslation();
  return (
    <div role="status" aria-live="polite" className="mx-auto w-full max-w-md py-12">
      <div className="flex flex-col items-center gap-3 text-center">
        <Loader2 className="size-8 animate-spin text-primary" aria-hidden="true" />
        <p className="font-heading text-lg font-medium">{t('backend.starting')}</p>
        <p className="text-sm text-muted-foreground">{t('backend.startingHint')}</p>
      </div>
      <div className="mt-8 space-y-3" aria-hidden="true">
        <Skeleton className="h-6 w-1/2" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    </div>
  );
}

function BackendCrashed({ message }: { message: string }) {
  const { t } = useTranslation();
  return (
    <div className="mx-auto w-full max-w-md py-12">
      <Card role="alert">
        <CardHeader>
          <div className="mb-2 flex size-10 items-center justify-center rounded-full bg-destructive/10 text-destructive">
            <ServerCrash className="size-5" aria-hidden="true" />
          </div>
          <CardTitle>
            <h1>{t('backend.crashedTitle')}</h1>
          </CardTitle>
          <CardDescription>{t('backend.crashedDescription')}</CardDescription>
        </CardHeader>
        <CardContent>
          <p className="rounded-md bg-muted px-3 py-2 font-mono text-xs break-words text-muted-foreground">
            {t('backend.details', { message })}
          </p>
        </CardContent>
        <CardFooter className="flex-wrap gap-2">
          <Button onClick={() => window.location.reload()}>
            <RotateCcw aria-hidden="true" />
            {t('backend.reload')}
          </Button>
          {/* The saved data may be what crashed it: offer a fresh start. */}
          {demoControls && (
            <ResetDemoDialog>
              <Button variant="outline">{t('backend.reset')}</Button>
            </ResetDemoDialog>
          )}
        </CardFooter>
      </Card>
    </div>
  );
}

/** The web demo runs in another tab; "Use here" moves it to this one. */
function DemoElsewhere() {
  const { t } = useTranslation();
  return (
    <div className="mx-auto w-full max-w-md py-12">
      <Card role="status">
        <CardHeader>
          <div className="mb-2 flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
            <AppWindow className="size-5" aria-hidden="true" />
          </div>
          <CardTitle>
            <h1>{t('demo.elsewhere.title')}</h1>
          </CardTitle>
          <CardDescription>{t('demo.elsewhere.description')}</CardDescription>
        </CardHeader>
        <CardFooter>
          <Button onClick={() => demoControls?.takeOver()}>{t('demo.elsewhere.useHere')}</Button>
        </CardFooter>
      </Card>
    </div>
  );
}
