import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { MailCheck, Send } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { requestPasswordResetMutation } from '@/api/queries';
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
import { FormError } from './FormError';
import { forgotPasswordSchema, invalidFields, useFieldErrorText } from './form';
import { TextField } from './TextField';

type ResetRequest = z.infer<typeof forgotPasswordSchema>;

/**
 * Asks for a link to set a new password (/forgot-password). The API answers
 * alike for every address, so the page does too: "check your inbox".
 */
export function ForgotPasswordPage() {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const request = useMutation(requestPasswordResetMutation(queryClient));
  const errorText = useFieldErrorText();
  const form = useForm<ResetRequest>({
    resolver: zodResolver(forgotPasswordSchema),
    defaultValues: { email: '' },
  });

  const onSubmit = ({ email }: ResetRequest) =>
    request.mutate(
      { email, locale: currentLanguage() },
      {
        onError: (error) => {
          for (const field of invalidFields(error, ['email'] as const)) {
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
            {t('forgotPassword.title')}
          </h1>
        </CardTitle>
        <CardDescription>{t('forgotPassword.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {request.isSuccess ? (
          <Alert role="status">
            <MailCheck aria-hidden="true" />
            <AlertTitle>{t('forgotPassword.successTitle')}</AlertTitle>
            <AlertDescription>
              <p>{t('forgotPassword.success', { email: request.variables.email })}</p>
              <DemoMailboxHint />
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <FormError error={request.error} messages={{ 422: t('form.checkEntries') }} />
            <form noValidate className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
              <TextField
                label={t('form.email')}
                type="email"
                autoComplete="username"
                error={errorText(form.formState.errors.email)}
                {...form.register('email')}
              />
              <Button type="submit" className="w-full" disabled={request.isPending}>
                <Send aria-hidden="true" />
                {request.isPending ? t('forgotPassword.submitting') : t('forgotPassword.submit')}
              </Button>
            </form>
          </>
        )}
      </CardContent>
      <CardFooter className="justify-center text-sm">
        <Link to="/login" className="font-medium text-primary underline-offset-4 hover:underline">
          {t('forgotPassword.backToLogin')}
        </Link>
      </CardFooter>
    </Card>
  );
}
