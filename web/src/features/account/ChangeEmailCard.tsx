import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { MailCheck, Send } from 'lucide-react';
import { useId, useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import type { z } from 'zod';
import type { Me } from '@/api/client';
import { requestEmailChangeMutation } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { FormError } from '@/features/auth/FormError';
import { changeEmailSchema, invalidFields, useFieldErrorText } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import { DemoMailboxHint } from '@/features/demo/DemoMailboxHint';
import { currentLanguage } from '@/i18n';

type EmailChange = z.infer<ReturnType<typeof changeEmailSchema>>;

/**
 * Requests a new email address: the API mails a confirmation link to it, and
 * the address changes only once that link is opened.
 */
export function ChangeEmailCard({ me }: { me: Me }) {
  const { t } = useTranslation('account');
  const { t: tAuth } = useTranslation('auth');
  const titleId = useId();
  const queryClient = useQueryClient();
  const change = useMutation(requestEmailChangeMutation(queryClient));
  const errorText = useFieldErrorText();
  const schema = useMemo(() => changeEmailSchema(me.email), [me.email]);
  const form = useForm<EmailChange>({
    resolver: zodResolver(schema),
    defaultValues: { newEmail: '', password: '' },
  });
  const { errors } = form.formState;

  const onSubmit = (values: EmailChange) =>
    change.mutate(
      { ...values, locale: currentLanguage() },
      {
        onSuccess: () => form.reset(),
        onError: (error) => {
          for (const field of invalidFields(error, ['newEmail', 'password'] as const)) {
            form.setError(field, {
              type: 'server',
              message: field === 'password' ? 'wrongPassword' : 'invalid',
            });
          }
        },
      },
    );

  return (
    <Card role="region" aria-labelledby={titleId}>
      <CardHeader>
        <CardTitle>
          <h2 id={titleId} className="font-heading text-lg font-semibold">
            {t('email.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('email.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <dl className="grid gap-0.5 text-sm">
          <dt className="text-muted-foreground">{t('email.current')}</dt>
          <dd className="font-medium break-all">{me.email}</dd>
        </dl>
        {change.isSuccess && (
          <Alert role="status">
            <MailCheck aria-hidden="true" />
            <AlertTitle>{t('email.successTitle')}</AlertTitle>
            <AlertDescription>
              <p>{t('email.success', { email: change.variables.newEmail })}</p>
              <DemoMailboxHint />
            </AlertDescription>
          </Alert>
        )}
        <FormError
          error={change.error}
          messages={{
            403: tAuth('form.forbiddenWhileImpersonating'),
            422: tAuth('form.checkEntries'),
          }}
        />
        <form noValidate className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
          <TextField
            label={t('email.new')}
            type="email"
            autoComplete="email"
            error={errorText(errors.newEmail)}
            {...form.register('newEmail')}
          />
          <TextField
            label={t('email.password')}
            type="password"
            autoComplete="current-password"
            hint={t('email.passwordHint')}
            error={errorText(errors.password)}
            {...form.register('password')}
          />
          <Button type="submit" disabled={change.isPending}>
            <Send aria-hidden="true" />
            {change.isPending ? t('email.submitting') : t('email.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
