import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { MailPlus } from 'lucide-react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { createPersonAccountMutation, meQuery, type PersonDetail } from '@/api/queries';
import { TextField } from '@/features/auth/TextField';
import { SelectField } from './crud/fields';
import { conflictReason } from '@/api/conflict';
import { showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetActions, FormSheetHeader } from './crud/FormSheet';
import { personName } from '@/lib/personName';
import { ProblemAlert } from '@/components/ProblemAlert';
import { accountRoles, accountSchema, type AccountValues } from './personForms';

/**
 * A sheet that creates an account for a person without one (`target`;
 * closed while it is null): the email it signs in with, which gets the
 * invitation, and its role. Only admins are offered the admin role.
 */
export function PersonAccountSheet({
  target,
  onClose,
}: {
  target: PersonDetail | null;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => <AccountForm key={shown.id} person={shown} onDone={onClose} />}
    </FormSheet>
  );
}

function AccountForm({ person, onDone }: { person: PersonDetail; onDone: () => void }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const me = useQuery(meQuery);
  const create = useMutation(createPersonAccountMutation(queryClient));
  const form = useForm<AccountValues>({
    resolver: zodResolver(accountSchema),
    defaultValues: { email: person.email ?? '', role: 'member' },
  });
  const { errors } = form.formState;
  const name = personName(person);
  const roles = accountRoles.filter((role) => role !== 'admin' || me.data?.role === 'admin');

  const onSubmit = (body: AccountValues) => {
    create.mutate(
      { personId: person.id, body },
      {
        onSuccess: (account) => {
          toast.success(t('persons.account.created', { name, email: account.email }));
          onDone();
        },
        onError: (error) => {
          showServerErrors(error, form, ['email', 'role']);
          // Only a taken email is the field's fault, not an account that exists already.
          if (conflictReason(error) === 'duplicate') {
            form.setError('email', { type: 'server', message: 'emailTaken' });
          }
        },
      },
    );
  };

  return (
    <>
      <FormSheetHeader
        title={t('persons.account.createTitle', { name })}
        description={t('persons.account.createDescription', { givenName: person.givenName })}
      />
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <ProblemAlert
          error={create.error}
          conflicts={{
            // The email belongs to another account; the field says so too.
            duplicate: t('persons.account.duplicate'),
            // Someone created an account meanwhile; the page behind is asked again.
            'has-account': t('persons.account.hasAccount', { name }),
          }}
          messages={{
            403: t('persons.account.forbidden'),
            404: t('persons.notFound'),
            422: tAuth('form.checkEntries'),
          }}
        />
        <TextField
          label={t('persons.fields.email')}
          type="email"
          autoComplete="off"
          hint={t('persons.account.emailHint')}
          error={errorText(errors.email)}
          {...form.register('email')}
        />
        <Controller
          control={form.control}
          name="role"
          render={({ field }) => (
            <SelectField
              label={t('persons.account.role')}
              placeholder={t('persons.account.chooseRole')}
              options={roles.map((role) => ({ value: role, label: tAuth(`roles.${role}`) }))}
              value={field.value}
              onChange={field.onChange}
              onBlur={field.onBlur}
              error={errorText(errors.role)}
            />
          )}
        />
        <FormSheetActions
          pending={create.isPending}
          submitLabel={t('persons.account.submit')}
          icon={<MailPlus aria-hidden="true" />}
        />
      </form>
    </>
  );
}
