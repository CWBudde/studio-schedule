import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { CircleAlert, Save } from 'lucide-react';
import { useId, useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { Me } from '@/api/client';
import { familyQuery, type FamilyPerson, updateMyPersonMutation } from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import { FormError } from '@/features/auth/FormError';
import { EmailLanguageField } from './EmailLanguageField';
import { PersonFields } from './PersonFields';
import {
  personData,
  personSchema,
  type PersonValues,
  personValues,
  showPersonErrors,
  useSchoolToday,
} from './personForm';

/**
 * The signed-in person's own data (name, phone, birth date) and the language
 * of the mails the account receives. Also while an admin is signed in as
 * them: the API allows it, for support.
 */
export function ProfileCard({ me }: { me: Me }) {
  const { t } = useTranslation('account');
  const titleId = useId();
  const family = useQuery(familyQuery);

  return (
    <Card role="region" aria-labelledby={titleId}>
      <CardHeader>
        <CardTitle>
          <h2 id={titleId} className="font-heading text-lg font-semibold">
            {t('profile.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('profile.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {family.isPending ? (
          <div role="status" className="space-y-3">
            <span className="sr-only">{t('profile.loading')}</span>
            <Skeleton className="h-36 w-full" aria-hidden="true" />
          </div>
        ) : family.isError ? (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('profile.loadFailed')}</AlertTitle>
          </Alert>
        ) : (
          <ProfileForm key={family.data.me.id} person={family.data.me} />
        )}
        <Separator />
        <EmailLanguageField me={me} />
      </CardContent>
    </Card>
  );
}

function ProfileForm({ person }: { person: FamilyPerson }) {
  const { t } = useTranslation('account');
  const { t: tAuth } = useTranslation('auth');
  const queryClient = useQueryClient();
  const update = useMutation(updateMyPersonMutation(queryClient));
  const today = useSchoolToday();
  const schema = useMemo(() => personSchema(today), [today]);
  const form = useForm<PersonValues>({
    resolver: zodResolver(schema),
    defaultValues: personValues(person),
  });

  const onSubmit = (values: PersonValues) =>
    update.mutate(personData(values), {
      onSuccess: () => {
        form.reset(values);
        toast.success(t('profile.saved'));
      },
      onError: (error) => showPersonErrors(error, form),
    });

  return (
    <form noValidate className="space-y-4" onSubmit={form.handleSubmit(onSubmit)}>
      <FormError error={update.error} messages={{ 422: tAuth('form.checkEntries') }} />
      <PersonFields form={form} today={today} self className="sm:grid-cols-2" />
      <Button type="submit" disabled={update.isPending}>
        <Save aria-hidden="true" />
        {update.isPending ? t('profile.submitting') : t('profile.submit')}
      </Button>
    </form>
  );
}
