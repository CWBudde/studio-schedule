import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate } from '@tanstack/react-router';
import { CircleAlert, KeyRound, LogIn } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { DemoLogin, LoginBody, Me } from '@/api/client';
import { demoLoginsQuery, loginMutation, meQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { DemoMailboxHint } from '@/features/demo/DemoMailboxHint';
import { loginSchema, statusOf, useFieldErrorText } from './form';
import { TextField } from './TextField';

export function LoginPage() {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const login = useMutation(loginMutation(queryClient));

  const signIn = (body: LoginBody) =>
    login.mutate(body, {
      onSuccess: (account) => {
        toast.success(t('login.welcome', { name: account.givenName }));
        void navigate({ to: '/' });
      },
    });

  if (me.data && !login.isSuccess) return <SignedIn me={me.data} />;

  return (
    <div className="space-y-8">
      <Card className="mx-auto w-full max-w-md">
        <CardHeader>
          <CardTitle>
            <h1 className="font-heading text-2xl font-semibold tracking-tight">
              {t('login.title')}
            </h1>
          </CardTitle>
          <CardDescription>{t('login.description')}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <LoginError status={statusOf(login.error)} />
          <LoginForm onSubmit={signIn} pending={login.isPending} />
        </CardContent>
        <CardFooter className="justify-center gap-1 text-sm text-muted-foreground">
          {t('login.noAccount')}
          <Link
            to="/register"
            className="font-medium text-primary underline-offset-4 hover:underline"
          >
            {t('login.register')}
          </Link>
        </CardFooter>
      </Card>
      <DemoLogins onSignIn={signIn} pending={login.isPending} />
    </div>
  );
}

function LoginForm({
  onSubmit,
  pending,
}: {
  onSubmit: (body: LoginBody) => void;
  pending: boolean;
}) {
  const { t } = useTranslation('auth');
  const errorText = useFieldErrorText();
  const form = useForm<LoginBody>({
    resolver: zodResolver(loginSchema),
    defaultValues: { email: '', password: '' },
  });
  const { errors } = form.formState;

  return (
    <form noValidate className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
      <TextField
        label={t('form.email')}
        type="email"
        autoComplete="username"
        error={errorText(errors.email)}
        {...form.register('email')}
      />
      <TextField
        label={t('form.password')}
        type="password"
        autoComplete="current-password"
        error={errorText(errors.password)}
        {...form.register('password')}
      />
      <Button type="submit" className="w-full" disabled={pending}>
        <LogIn aria-hidden="true" />
        {pending ? t('login.submitting') : t('login.submit')}
      </Button>
    </form>
  );
}

/** The answer of a failed sign-in: wrong credentials, unconfirmed email, or anything else. */
function LoginError({ status }: { status: number | undefined }) {
  const { t } = useTranslation('auth');
  if (status === undefined) return null;

  if (status === 403) {
    return (
      <Alert>
        <CircleAlert aria-hidden="true" />
        <AlertTitle>{t('login.notConfirmedTitle')}</AlertTitle>
        <AlertDescription>
          <p>{t('login.notConfirmed')}</p>
          <DemoMailboxHint />
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{status === 401 ? t('login.invalidCredentials') : t('form.failed')}</AlertTitle>
    </Alert>
  );
}

/** The fictional demo accounts with their passwords; absent outside a demo. */
function DemoLogins({
  onSignIn,
  pending,
}: {
  onSignIn: (body: LoginBody) => void;
  pending: boolean;
}) {
  const { t } = useTranslation('auth');
  const logins = useQuery(demoLoginsQuery);
  if (!logins.data?.length) return null;

  return (
    <Card role="region" aria-labelledby="demo-logins-title" className="mx-auto w-full max-w-3xl">
      <CardHeader>
        <CardTitle>
          <h2 id="demo-logins-title" className="flex items-center gap-2">
            <KeyRound className="size-4" aria-hidden="true" />
            {t('demoLogins.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('demoLogins.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="divide-y">
          {logins.data.map((login) => (
            <DemoLoginRow
              key={login.email}
              login={login}
              pending={pending}
              onSignIn={() => onSignIn({ email: login.email, password: login.password })}
            />
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}

function DemoLoginRow({
  login,
  pending,
  onSignIn,
}: {
  login: DemoLogin;
  pending: boolean;
  onSignIn: () => void;
}) {
  const { t } = useTranslation('auth');
  const name = `${login.givenName} ${login.familyName}`;
  const role = t(`roles.${login.role}`);

  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 py-3 first:pt-0 last:pb-0">
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{name}</span>
          <Badge variant="secondary">{role}</Badge>
        </div>
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 text-xs text-muted-foreground">
          <dt>{t('demoLogins.email')}</dt>
          <dd className="min-w-0 truncate font-mono">{login.email}</dd>
          <dt>{t('demoLogins.password')}</dt>
          <dd className="font-mono">{login.password}</dd>
        </dl>
      </div>
      <Button
        variant="outline"
        size="sm"
        disabled={pending}
        aria-label={t('demoLogins.signInAs', { name, role })}
        onClick={onSignIn}
      >
        <LogIn aria-hidden="true" />
        {t('demoLogins.signIn')}
      </Button>
    </li>
  );
}

function SignedIn({ me }: { me: Me }) {
  const { t } = useTranslation('auth');
  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        <CardTitle>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">{t('login.title')}</h1>
        </CardTitle>
        <CardDescription>
          {t('login.alreadySignedIn', { name: `${me.givenName} ${me.familyName}` })}
        </CardDescription>
      </CardHeader>
      <CardFooter>
        <Button asChild variant="outline">
          <Link to="/">{t('login.toHome')}</Link>
        </Button>
      </CardFooter>
    </Card>
  );
}
