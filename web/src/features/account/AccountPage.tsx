import { useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { LogIn, UserRoundX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { Me } from '@/api/client';
import { meQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { FormError } from '@/features/auth/FormError';
import { ChangeEmailCard } from './ChangeEmailCard';
import { ChangePasswordCard } from './ChangePasswordCard';

/**
 * The signed-in person's account settings (/account): password and email
 * address. Signed out, the page asks to sign in; while an admin is signed in
 * as someone else, it explains that the API refuses these changes.
 */
export function AccountPage() {
  const { t } = useTranslation('account');
  const me = useQuery(meQuery);
  const editable = me.data && !me.data.impersonator;

  return (
    <div className="mx-auto w-full max-w-2xl space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        {editable && <p className="max-w-prose text-muted-foreground">{t('description')}</p>}
      </div>
      {me.isPending ? (
        <AccountLoading />
      ) : me.isError ? (
        <FormError error={me.error} />
      ) : !me.data ? (
        <SignedOut />
      ) : me.data.impersonator ? (
        <Impersonating me={me.data} />
      ) : (
        <>
          <ChangePasswordCard />
          <ChangeEmailCard me={me.data} />
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

function Impersonating({ me }: { me: Me }) {
  const { t } = useTranslation('account');
  return (
    <Alert>
      <UserRoundX aria-hidden="true" />
      <AlertTitle>{t('impersonatingTitle')}</AlertTitle>
      <AlertDescription>
        <p>{t('impersonating', { name: `${me.givenName} ${me.familyName}` })}</p>
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
