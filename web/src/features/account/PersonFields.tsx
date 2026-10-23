import type { UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';
import { TextField } from '@/features/auth/TextField';
import type { SchoolDate } from '@/lib/schoolTime';
import { type PersonValues, usePersonErrorText } from './personForm';

/**
 * Name, phone and birth date of a person. `self` is the signed-in person
 * (the browser may fill in their data); a child's fields are not filled in.
 */
export function PersonFields({
  form,
  today,
  self,
  className,
}: {
  form: UseFormReturn<PersonValues>;
  today: SchoolDate | undefined;
  self: boolean;
  className?: string;
}) {
  const { t } = useTranslation('account');
  const errorText = usePersonErrorText();
  const { errors } = form.formState;
  const autoComplete = (token: string) => (self ? token : 'off');

  return (
    <div className={cn('grid gap-4', className)}>
      <TextField
        label={t('person.givenName')}
        autoComplete={autoComplete('given-name')}
        error={errorText(errors.givenName)}
        {...form.register('givenName')}
      />
      <TextField
        label={t('person.familyName')}
        autoComplete={autoComplete('family-name')}
        error={errorText(errors.familyName)}
        {...form.register('familyName')}
      />
      <TextField
        label={t('person.phone')}
        type="tel"
        autoComplete={autoComplete('tel')}
        error={errorText(errors.phone)}
        {...form.register('phone')}
      />
      <TextField
        label={t('person.birthDate')}
        type="date"
        max={today}
        autoComplete={autoComplete('bday')}
        error={errorText(errors.birthDate)}
        {...form.register('birthDate')}
      />
    </div>
  );
}
