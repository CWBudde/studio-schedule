import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useSearch } from '@tanstack/react-router';
import { CircleCheck, KeyRound, LinkIcon } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { resetPasswordMutation } from '@/api/queries';
import { LinkPageCard } from '@/components/LinkPageCard';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { FormError } from './FormError';
import { invalidFields, resetPasswordSchema, statusOf, useFieldErrorText } from './form';
import { TextField } from './TextField';

type NewPassword = z.infer<typeof resetPasswordSchema>;

/**
 * Sets a new password with the token of the mailed reset link
 * (/reset-password?token=…). It does not sign in: the page links to the
 * sign-in afterwards.
 */
export function ResetPasswordPage() {
  const { t } = useTranslation('auth');
  const { token } = useSearch({ from: '/reset-password' });
  const queryClient = useQueryClient();
  const reset = useMutation(resetPasswordMutation(queryClient));
  const errorText = useFieldErrorText();
  const form = useForm<NewPassword>({
    resolver: zodResolver(resetPasswordSchema),
    defaultValues: { password: '', repeat: '' },
  });
  const { errors } = form.formState;

  if (!token || statusOf(reset.error) === 410) return <InvalidLink />;
  if (reset.isSuccess) return <PasswordChanged />;

  const onSubmit = ({ password }: NewPassword) =>
    reset.mutate(
      { token, password },
      {
        onError: (error) => {
          for (const field of invalidFields(error, ['password'] as const)) {
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
            {t('resetPassword.title')}
          </h1>
        </CardTitle>
        <CardDescription>{t('resetPassword.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormError error={reset.error} messages={{ 422: t('form.checkEntries') }} />
        <form noValidate className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
          <TextField
            label={t('form.newPassword')}
            type="password"
            autoComplete="new-password"
            hint={t('register.passwordHint')}
            error={errorText(errors.password)}
            {...form.register('password')}
          />
          <TextField
            label={t('form.passwordRepeat')}
            type="password"
            autoComplete="new-password"
            error={errorText(errors.repeat)}
            {...form.register('repeat')}
          />
          <Button type="submit" className="w-full" disabled={reset.isPending}>
            <KeyRound aria-hidden="true" />
            {reset.isPending ? t('resetPassword.submitting') : t('resetPassword.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function PasswordChanged() {
  const { t } = useTranslation('auth');
  return (
    <LinkPageCard
      icon={<CircleCheck />}
      title={t('resetPassword.successTitle')}
      description={t('resetPassword.success')}
      status
    >
      <CardFooter>
        <Button asChild>
          <Link to="/login">{t('resetPassword.signIn')}</Link>
        </Button>
      </CardFooter>
    </LinkPageCard>
  );
}

function InvalidLink() {
  const { t } = useTranslation('auth');
  return (
    <LinkPageCard
      icon={<LinkIcon />}
      tone="destructive"
      title={t('resetPassword.invalidTitle')}
      description={t('resetPassword.invalid')}
    >
      <CardFooter>
        <Button asChild>
          <Link to="/forgot-password">{t('resetPassword.requestNew')}</Link>
        </Button>
      </CardFooter>
    </LinkPageCard>
  );
}
