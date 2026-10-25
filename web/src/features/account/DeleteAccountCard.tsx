import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { UserRoundX } from 'lucide-react';
import { useId, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { z } from 'zod';
import { deleteMyAccountMutation } from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { deleteAccountSchema, invalidFields, useFieldErrorText } from '@/features/auth/form';
import { TextField } from '@/features/auth/TextField';
import { currentLanguage } from '@/i18n';

type Confirmation = z.infer<typeof deleteAccountSchema>;

/**
 * Deletes the signed-in account (ADR 0016) after the password and a
 * confirmation: the person and its bookings stay with the school, the
 * account, its sign-ins and calendar links go, and a confirmation is mailed.
 * Afterwards nobody is signed in.
 */
export function DeleteAccountCard() {
  const { t } = useTranslation('account');
  const { t: tAuth } = useTranslation('auth');
  const titleId = useId();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const remove = useMutation(deleteMyAccountMutation(queryClient));
  const errorText = useFieldErrorText();
  const [confirming, setConfirming] = useState<Confirmation | null>(null);
  const form = useForm<Confirmation>({
    resolver: zodResolver(deleteAccountSchema),
    defaultValues: { password: '' },
  });
  const { errors } = form.formState;

  // Once it succeeds, nobody is signed in: the page may unmount before
  // mutate()'s own callbacks run, so the toast and the navigation follow
  // the promise of mutateAsync.
  const onConfirm = async (values: Confirmation) => {
    try {
      await remove.mutateAsync({ ...values, locale: currentLanguage() });
    } catch (error) {
      setConfirming(null);
      if (invalidFields(error, ['password'] as const).length > 0) {
        form.setError('password', { type: 'server', message: 'wrongPassword' });
      }
      return;
    }
    toast.success(t('deleteAccount.deleted'));
    void navigate({ to: '/' });
  };

  return (
    <Card role="region" aria-labelledby={titleId} className="ring-destructive/30">
      <CardHeader>
        <CardTitle>
          <h2 id={titleId} className="font-heading text-lg font-semibold">
            {t('deleteAccount.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('deleteAccount.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {!confirming && (
          <ProblemAlert
            error={remove.error}
            conflicts={{
              'last-admin': t('deleteAccount.lastAdmin'),
              'in-use': t('deleteAccount.inUse'),
            }}
            messages={{
              403: tAuth('form.forbiddenWhileImpersonating'),
              422: tAuth('form.checkEntries'),
            }}
          />
        )}
        <form
          noValidate
          className="space-y-4"
          onSubmit={form.handleSubmit((values) => setConfirming(values))}
        >
          <TextField
            label={t('deleteAccount.password')}
            type="password"
            autoComplete="current-password"
            error={errorText(errors.password)}
            {...form.register('password')}
          />
          <Button type="submit" variant="destructive" disabled={remove.isPending}>
            <UserRoundX aria-hidden="true" />
            {t('deleteAccount.submit')}
          </Button>
        </form>
      </CardContent>
      <AlertDialog
        open={confirming !== null}
        onOpenChange={(open) => {
          if (!open && !remove.isPending) setConfirming(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('deleteAccount.confirmTitle')}</AlertDialogTitle>
            <AlertDialogDescription>{t('deleteAccount.confirmDescription')}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={remove.isPending}>
              {t('deleteAccount.cancel')}
            </AlertDialogCancel>
            {/* No AlertDialogAction: it would close the dialog before the answer. */}
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => confirming && void onConfirm(confirming)}
            >
              <UserRoundX aria-hidden="true" />
              {remove.isPending ? t('deleteAccount.deleting') : t('deleteAccount.confirm')}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}
