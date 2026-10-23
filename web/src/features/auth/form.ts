import type { FieldError } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import type { ConfirmEmailBody, LoginBody, RegisterBody } from '@/api/client';
import { ApiError } from '@/api/queries';

/** Validation messages are keys of `auth:form.errors`, translated when shown. */
const fieldErrors = ['required', 'email', 'passwordMin', 'tooLong', 'invalid'] as const;
type FieldErrorKey = (typeof fieldErrors)[number];

const isFieldErrorKey = (key: unknown): key is FieldErrorKey =>
  fieldErrors.includes(key as FieldErrorKey);

// Limits as in the API (RegisterInput, LoginInput, ConfirmEmailInput).
const name = z.string().trim().min(1, 'required').max(200, 'tooLong');
const email = z.string().trim().min(1, 'required').max(254, 'tooLong').pipe(z.email('email'));
const password = z.string().min(1, 'required').max(256, 'tooLong');

export const loginSchema = z.object({ email, password }) satisfies z.ZodType<LoginBody>;

export const registerSchema = z.object({
  givenName: name,
  familyName: name,
  email,
  password: password.min(8, 'passwordMin'),
}) satisfies z.ZodType<Omit<RegisterBody, 'locale'>>;

export const confirmSchema = z.object({ password }) satisfies z.ZodType<
  Omit<ConfirmEmailBody, 'token'>
>;

/** Translates a react-hook-form field error whose message is a FieldErrorKey. */
export function useFieldErrorText() {
  const { t } = useTranslation('auth');
  return (error: FieldError | undefined): string | undefined => {
    if (!error) return undefined;
    return t(`form.errors.${isFieldErrorKey(error.message) ? error.message : 'invalid'}`);
  };
}

/** The HTTP status of a failed API call, if it was one. */
export const statusOf = (error: unknown): number | undefined =>
  error instanceof ApiError ? error.status : undefined;

/**
 * The request body fields a 422 problem+json answer names (from locations
 * such as 'body.email'), restricted to the fields of the form.
 */
export function invalidFields<F extends string>(error: unknown, fields: readonly F[]): F[] {
  if (!(error instanceof ApiError) || error.status !== 422) return [];
  const body = error.body as { errors?: { location?: string }[] | null } | undefined;
  const named = (body?.errors ?? []).map((e) => e.location?.replace(/^body\./, ''));
  return fields.filter((f) => named.includes(f));
}
