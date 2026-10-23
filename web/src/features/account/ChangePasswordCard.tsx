import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CircleCheck, KeyRound } from 'lucide-react';
import { useId } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import type { z } from 'zod';
import { changePasswordMutation } from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { FormError } from '@/features/auth/FormError';
import { changePasswordSchema, invalidFields, useFieldErrorText } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';

type PasswordChange = z.infer<typeof changePasswordSchema>;

/**
 * Changes the password after checking the current one. This session stays;
 * the API ends every other session of the account.
 */
export function ChangePasswordCard() {
  const { t } = useTranslation('account');
  const { t: tAuth } = useTranslation('auth');
  const titleId = useId();
  const queryClient = useQueryClient();
  const change = useMutation(changePasswordMutation(queryClient));
  const errorText = useFieldErrorText();
  const form = useForm<PasswordChange>({
    resolver: zodResolver(changePasswordSchema),
    defaultValues: { currentPassword: '', newPassword: '', repeat: '' },
  });
  const { errors } = form.formState;

  const onSubmit = ({ currentPassword, newPassword }: PasswordChange) =>
    change.mutate(
      { currentPassword, newPassword },
      {
        onSuccess: () => form.reset(),
        onError: (error) => {
          for (const field of invalidFields(error, ['currentPassword', 'newPassword'] as const)) {
            form.setError(field, {
              type: 'server',
              message: field === 'currentPassword' ? 'wrongPassword' : 'invalid',
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
            {t('password.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('password.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {change.isSuccess && (
          <Alert role="status">
            <CircleCheck aria-hidden="true" />
            <AlertTitle>{t('password.success')}</AlertTitle>
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
            label={t('password.current')}
            type="password"
            autoComplete="current-password"
            error={errorText(errors.currentPassword)}
            {...form.register('currentPassword')}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField
              label={t('password.new')}
              type="password"
              autoComplete="new-password"
              hint={t('password.hint')}
              error={errorText(errors.newPassword)}
              {...form.register('newPassword')}
            />
            <TextField
              label={t('password.repeat')}
              type="password"
              autoComplete="new-password"
              error={errorText(errors.repeat)}
              {...form.register('repeat')}
            />
          </div>
          <Button type="submit" disabled={change.isPending}>
            <KeyRound aria-hidden="true" />
            {change.isPending ? t('password.submitting') : t('password.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
