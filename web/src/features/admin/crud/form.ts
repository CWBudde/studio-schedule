import type { FieldError, FieldValues, Path, UseFormReturn } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { conflictReason } from '@/api/conflict';
import { invalidFields, useFieldErrorText } from '@/features/auth/form';

/**
 * Marks the fields of `form` that a refused write names: the fields of a
 * 422 answer, and `nameField` for a 409 `duplicate`.
 */
export function showServerErrors<V extends FieldValues>(
  error: unknown,
  form: Pick<UseFormReturn<V>, 'setError'>,
  fields: readonly Path<V>[],
  nameField?: Path<V>,
) {
  for (const field of invalidFields(error, fields)) {
    form.setError(field, { type: 'server', message: 'invalid' });
  }
  if (nameField && conflictReason(error) === 'duplicate') {
    form.setError(nameField, { type: 'server', message: 'duplicate' });
  }
}

/** Validation messages of the admin forms, keys of `admin:form.errors`. */
const adminErrors = [
  'capacity',
  'location',
  'person',
  'duplicate',
  'choose',
  'startTime',
  'duration',
  'midnight',
  'accompanist',
  'bookedCapacity',
  'endBeforeStart',
  'termLessons',
  'choosePerson',
  'alreadyMember',
  'birthDateFuture',
  'selfGuardian',
  'alreadyGuardian',
  'guardianCycle',
  'guardianInvalid',
  'emailTaken',
] as const;
type AdminErrorKey = (typeof adminErrors)[number];
const isAdminErrorKey = (key: unknown): key is AdminErrorKey =>
  adminErrors.includes(key as AdminErrorKey);

/**
 * Translates an admin form's field error: its own keys or the auth form's
 * (required, tooLong, invalid, …).
 */
export function useAdminErrorText() {
  const { t } = useTranslation('admin');
  const errorText = useFieldErrorText();
  return (error: FieldError | undefined): string | undefined => {
    return isAdminErrorKey(error?.message) ? t(`form.errors.${error.message}`) : errorText(error);
  };
}
