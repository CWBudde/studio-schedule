import { useQuery } from '@tanstack/react-query';
import type { FieldError, UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { ApiError, type FamilyPerson, type PersonData, publicSettingsQuery } from '@/api/queries';
import { invalidFields, useFieldErrorText } from '@/features/auth/form';
import { type SchoolDate, todayIn } from '@/lib/schoolTime';

// Limits as in the API (PersonData).
const name = z.string().trim().min(1, 'required').max(100, 'tooLong');

/**
 * The person data of the profile and dependent forms. Empty phone and birth
 * date mean "none". The birth date must not be after `today` (the school's
 * date); while that is unknown, the API decides.
 */
export const personSchema = (today: SchoolDate | undefined) =>
  z.object({
    givenName: name,
    familyName: name,
    phone: z.string().trim().max(50, 'tooLong'),
    birthDate: z.string().refine((d) => !today || d === '' || d <= today, 'birthDateFuture'),
  });

export type PersonValues = z.infer<ReturnType<typeof personSchema>>;

/** The form values of a person (or of a new one with the given fields). */
export const personValues = (person: Partial<FamilyPerson>): PersonValues => ({
  givenName: person.givenName ?? '',
  familyName: person.familyName ?? '',
  phone: person.phone ?? '',
  birthDate: person.birthDate ?? '',
});

/** The request body of the form values: an empty phone or birth date is left out. */
export const personData = ({
  givenName,
  familyName,
  phone,
  birthDate,
}: PersonValues): PersonData => ({
  givenName,
  familyName,
  ...(phone ? { phone } : {}),
  ...(birthDate ? { birthDate } : {}),
});

export const fullName = (person: { givenName: string; familyName: string }) =>
  `${person.givenName} ${person.familyName}`;

/** Today in the school's time zone; undefined until the settings are there. */
export function useSchoolToday(): SchoolDate | undefined {
  const settings = useQuery(publicSettingsQuery);
  return settings.data ? todayIn(settings.data.timezone) : undefined;
}

const personFields = ['givenName', 'familyName', 'phone', 'birthDate'] as const;

/** The API's check of the birth date names no field, only the reason in the detail. */
const isFutureBirthDate = (error: unknown) =>
  error instanceof ApiError &&
  error.status === 422 &&
  /birth date must not be in the future/.test(
    String((error.body as { detail?: unknown } | undefined)?.detail ?? ''),
  );

/** Marks the fields a 422 answer of a person operation refuses. */
export function showPersonErrors(error: unknown, form: UseFormReturn<PersonValues>) {
  for (const field of invalidFields(error, personFields)) {
    form.setError(field, { type: 'server', message: 'invalid' });
  }
  if (isFutureBirthDate(error)) {
    form.setError('birthDate', { type: 'server', message: 'birthDateFuture' });
  }
}

/** Translates a person form's field error (see useFieldErrorText). */
export function usePersonErrorText() {
  const { t } = useTranslation('account');
  const errorText = useFieldErrorText();
  return (error: FieldError | undefined): string | undefined =>
    error?.message === 'birthDateFuture' ? t('person.errors.birthDateFuture') : errorText(error);
}
