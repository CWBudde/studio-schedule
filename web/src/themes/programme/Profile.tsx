import { Link } from '@tanstack/react-router';
import { LogIn, UserRoundX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { FormError } from '@/features/auth/FormError';
import type { ProfileProps } from '../slots';

/** The account page: its sections one below the other in a narrow column. */
export function Profile(props: ProfileProps) {
  const { t } = useTranslation('account');

  return (
    <div className="mx-auto w-full max-w-2xl space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        {props.state === 'ready' && (
          <p className="max-w-prose text-muted-foreground">{t('description')}</p>
        )}
      </div>
      {props.state === 'loading' ? (
        <AccountLoading />
      ) : props.state === 'error' ? (
        <FormError error={props.error} />
      ) : props.state === 'signedOut' ? (
        <SignedOut />
      ) : (
        <>
          {props.sections.profile}
          {props.sections.dependents}
          {props.sections.calendarFeeds}
          {props.sections.push}
          {props.sections.myData}
          {props.impersonating ? (
            <Impersonating name={props.impersonating.name} />
          ) : (
            <>
              {props.sections.password}
              {props.sections.email}
              {props.sections.deleteAccount}
            </>
          )}
        </>
      )}
    </div>
  );
}

function SignedOut() {
  const { t } = useTranslation('account');
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-10 text-center">
      <LogIn className="size-8 text-muted-foreground" aria-hidden="true" />
      <p className="font-heading text-lg font-semibold">{t('signedOutTitle')}</p>
      <p className="max-w-prose text-muted-foreground">{t('signedOut')}</p>
      <Button asChild>
        <Link to="/login">{t('signIn')}</Link>
      </Button>
    </div>
  );
}

function Impersonating({ name }: { name: string }) {
  const { t } = useTranslation('account');
  return (
    <Alert>
      <UserRoundX aria-hidden="true" />
      <AlertTitle>{t('impersonatingTitle')}</AlertTitle>
      <AlertDescription>
        <p>{t('impersonating', { name })}</p>
      </AlertDescription>
    </Alert>
  );
}

function AccountLoading() {
  const { t } = useTranslation('account');
  return (
    <div role="status" className="space-y-3">
      <span className="sr-only">{t('loading')}</span>
      <Skeleton className="h-48 w-full" aria-hidden="true" />
      <Skeleton className="h-40 w-full" aria-hidden="true" />
    </div>
  );
}
