import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { CircleAlert, LinkIcon, MailCheck } from 'lucide-react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { z } from 'zod';
import { confirmEmailMutation } from '@/api/queries';
import { LinkPageCard } from '@/components/LinkPageCard';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { confirmSchema, statusOf, useFieldErrorText } from './form';
import { TextField } from './TextField';

type Confirmation = z.infer<typeof confirmSchema>;

/** Confirms the email address of a registration with the token of the mailed link. */
export function ConfirmPage() {
  const { t } = useTranslation('auth');
  const { token } = useSearch({ from: '/confirm' });
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const confirm = useMutation(confirmEmailMutation(queryClient));
  const errorText = useFieldErrorText();
  const form = useForm<Confirmation>({
    resolver: zodResolver(confirmSchema),
    defaultValues: { password: '' },
  });
  const status = statusOf(confirm.error);

  if (!token || status === 410) return <InvalidLink />;

  const onSubmit = ({ password }: Confirmation) =>
    confirm.mutate(
      { token, password },
      {
        onSuccess: (me) => {
          toast.success(t('confirm.success', { name: me.givenName }));
          void navigate({ to: '/' });
        },
      },
    );

  return (
    <Card className="mx-auto w-full max-w-md">
      <CardHeader>
        <CardTitle>
          <h1 className="font-heading text-2xl font-semibold tracking-tight">
            {t('confirm.title')}
          </h1>
        </CardTitle>
        <CardDescription>{t('confirm.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {status !== undefined && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>
              {status === 401 ? t('confirm.wrongPassword') : t('form.failed')}
            </AlertTitle>
          </Alert>
        )}
        <form noValidate className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
          <TextField
            label={t('form.password')}
            type="password"
            autoComplete="current-password"
            error={errorText(form.formState.errors.password)}
            {...form.register('password')}
          />
          <Button type="submit" className="w-full" disabled={confirm.isPending}>
            <MailCheck aria-hidden="true" />
            {confirm.isPending ? t('confirm.submitting') : t('confirm.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

function InvalidLink() {
  const { t } = useTranslation('auth');
  return (
    <LinkPageCard
      icon={<LinkIcon />}
      tone="destructive"
      title={t('confirm.invalidTitle')}
      description={t('confirm.invalid')}
    >
      <CardFooter>
        <Button asChild>
          <Link to="/register">{t('confirm.registerAgain')}</Link>
        </Button>
      </CardFooter>
    </LinkPageCard>
  );
}
