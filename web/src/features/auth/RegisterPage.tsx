import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CircleAlert, MailCheck, UserPlus } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { registerMutation } from '@/api/queries';
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
import { DemoMailboxHint } from '@/features/demo/DemoMailboxHint';
import { currentLanguage } from '@/i18n';
import { invalidFields, registerSchema, statusOf, useFieldErrorText } from './form';
import { TextField } from './TextField';

type Registration = z.infer<typeof registerSchema>;
const fields = ['givenName', 'familyName', 'email', 'password'] as const;

export function RegisterPage() {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const register = useMutation(registerMutation(queryClient));
  const errorText = useFieldErrorText();
  const form = useForm<Registration>({
    resolver: zodResolver(registerSchema),
    defaultValues: { givenName: '', familyName: '', email: '', password: '' },
  });
  const { errors } = form.formState;

  const onSubmit = (values: Registration) =>
    register.mutate(
      { ...values, locale: currentLanguage() },
      {
        onError: (error) => {
          for (const field of invalidFields(error, fields)) {
            form.setError(field, { type: 'server', message: 'invalid' });
          }
        },
      },
    );

  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        <CardTitle>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">
            {t('register.title')}
          </h1>
        </CardTitle>
        <CardDescription>{t('register.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {register.isSuccess ? (
          <Alert role="status">
            <MailCheck aria-hidden="true" />
            <AlertTitle>{t('register.successTitle')}</AlertTitle>
            <AlertDescription>
              <p>{t('register.success', { email: register.variables.email })}</p>
              <DemoMailboxHint />
            </AlertDescription>
          </Alert>
        ) : (
          <>
            {register.isError && (
              <Alert variant="destructive">
                <CircleAlert aria-hidden="true" />
                <AlertTitle>
                  {statusOf(register.error) === 422 ? t('form.checkEntries') : t('form.failed')}
                </AlertTitle>
              </Alert>
            )}
            <form noValidate className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
              <div className="grid gap-4 sm:grid-cols-2">
                <TextField
                  label={t('form.givenName')}
                  autoComplete="given-name"
                  error={errorText(errors.givenName)}
                  {...form.register('givenName')}
                />
                <TextField
                  label={t('form.familyName')}
                  autoComplete="family-name"
                  error={errorText(errors.familyName)}
                  {...form.register('familyName')}
                />
              </div>
              <TextField
                label={t('form.email')}
                type="email"
                autoComplete="email"
                error={errorText(errors.email)}
                {...form.register('email')}
              />
              <TextField
                label={t('form.password')}
                type="password"
                autoComplete="new-password"
                hint={t('register.passwordHint')}
                error={errorText(errors.password)}
                {...form.register('password')}
              />
              <Button type="submit" className="w-full" disabled={register.isPending}>
                <UserPlus aria-hidden="true" />
                {register.isPending ? t('register.submitting') : t('register.submit')}
              </Button>
            </form>
          </>
        )}
      </CardContent>
      <CardFooter className="justify-center gap-1 text-sm text-muted-foreground">
        {t('register.haveAccount')}
        <Link to="/login" className="font-medium text-primary underline-offset-4 hover:underline">
          {t('register.signIn')}
        </Link>
      </CardFooter>
    </Card>
  );
}
