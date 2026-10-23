import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useSearch } from '@tanstack/react-router';
import { CircleAlert, CircleCheck, LinkIcon, MailCheck } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { confirmEmailChangeMutation, meQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { currentLanguage } from '@/i18n';
import { FormError } from './FormError';
import { statusOf } from './form';

/**
 * Moves an account to its new address with the token of the mailed link
 * (/confirm-email-change?token=…), signed in or not. It waits for a click,
 * so that opening the link alone (e.g. by a mail scanner) changes nothing.
 */
export function ConfirmEmailChangePage() {
  const { t } = useTranslation('auth');
  const { token } = useSearch({ from: '/confirm-email-change' });
  const queryClient = useQueryClient();
  const me = useQuery(meQuery);
  const confirm = useMutation(confirmEmailChangeMutation(queryClient));
  // The address signed in when confirming: if it changed, the link was this
  // account's; otherwise it belonged to someone else.
  const [before, setBefore] = useState<string>();

  if (!token || statusOf(confirm.error) === 410) return <InvalidLink />;
  if (confirm.isSuccess) {
    const email = me.data && me.data.email !== before ? me.data.email : undefined;
    return <EmailChanged email={email} />;
  }

  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        <CardTitle>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">
            {t('confirmEmailChange.title')}
          </h1>
        </CardTitle>
        <CardDescription>{t('confirmEmailChange.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {statusOf(confirm.error) === 409 ? <AddressTaken /> : <FormError error={confirm.error} />}
        <Button
          className="w-full"
          disabled={confirm.isPending}
          onClick={() => {
            setBefore(me.data?.email);
            confirm.mutate({ token, locale: currentLanguage() });
          }}
        >
          <MailCheck aria-hidden="true" />
          {confirm.isPending ? t('confirmEmailChange.submitting') : t('confirmEmailChange.submit')}
        </Button>
      </CardContent>
    </Card>
  );
}

function AddressTaken() {
  const { t } = useTranslation('auth');
  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{t('confirmEmailChange.takenTitle')}</AlertTitle>
      <AlertDescription>
        <p>{t('confirmEmailChange.taken')}</p>
      </AlertDescription>
    </Alert>
  );
}

/** Done; `email` is the new address if the moved account is signed in here. */
function EmailChanged({ email }: { email: string | undefined }) {
  const { t } = useTranslation('auth');
  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        <div className="mb-2 flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
          <CircleCheck className="size-5" aria-hidden="true" />
        </div>
        <CardTitle>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">
            {t('confirmEmailChange.successTitle')}
          </h1>
        </CardTitle>
        <CardDescription role="status">
          {email
            ? t('confirmEmailChange.successSignedIn', { email })
            : t('confirmEmailChange.success')}
        </CardDescription>
      </CardHeader>
      <CardFooter>
        <Button asChild>
          {email ? (
            <Link to="/account">{t('confirmEmailChange.toAccount')}</Link>
          ) : (
            <Link to="/login">{t('confirmEmailChange.signIn')}</Link>
          )}
        </Button>
      </CardFooter>
    </Card>
  );
}

function InvalidLink() {
  const { t } = useTranslation('auth');
  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        <div className="mb-2 flex size-10 items-center justify-center rounded-full bg-destructive/10 text-destructive">
          <LinkIcon className="size-5" aria-hidden="true" />
        </div>
        <CardTitle>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">
            {t('confirmEmailChange.invalidTitle')}
          </h1>
        </CardTitle>
        <CardDescription>{t('confirmEmailChange.invalid')}</CardDescription>
      </CardHeader>
      <CardFooter>
        <Button asChild>
          <Link to="/account">{t('confirmEmailChange.toAccount')}</Link>
        </Button>
      </CardFooter>
    </Card>
  );
}
