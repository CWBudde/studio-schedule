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
import { CalendarFeedsCard } from '@/features/calendar-feeds/CalendarFeedsCard';
import { PushCard } from '@/features/push/PushCard';
import { ChangeEmailCard } from './ChangeEmailCard';
import { ChangePasswordCard } from './ChangePasswordCard';
import { DeleteAccountCard } from './DeleteAccountCard';
import { DependentsCard } from './DependentsCard';
import { MyDataCard } from './MyDataCard';
import { ProfileCard } from './ProfileCard';

/**
 * The signed-in person's account settings (/account): their person data and
 * mail language, their children, their calendar feeds, notifications on
 * this device, the download of
 * their data, the password, the email address, and deleting the account.
 * Signed out, the page asks to sign in. While an admin is signed in as
 * someone else, the person data and children stay editable (for support),
 * but the API refuses credential changes, which the page explains instead.
 */
export function AccountPage() {
  const { t } = useTranslation('account');
  const me = useQuery(meQuery);

  return (
    <div className="mx-auto w-full max-w-2xl space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        {me.data && <p className="max-w-prose text-muted-foreground">{t('description')}</p>}
      </div>
      {me.isPending ? (
        <AccountLoading />
      ) : me.isError ? (
        <FormError error={me.error} />
      ) : !me.data ? (
        <SignedOut />
      ) : (
        <>
          <ProfileCard me={me.data} />
          <DependentsCard />
          <CalendarFeedsCard />
          {/* An admin signed in as the account would get its notifications. */}
          {!me.data.impersonator && <PushCard accountId={me.data.accountId} />}
          <MyDataCard />
          {me.data.impersonator ? (
            <Impersonating me={me.data} />
          ) : (
            <>
              <ChangePasswordCard />
              <ChangeEmailCard me={me.data} />
              <DeleteAccountCard />
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
