import type { FieldError } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import type { ConfirmEmailBody, LoginBody, RegisterBody } from '@/api/client';
import {
  ApiError,
  type ChangePasswordBody,
  type RequestEmailChangeBody,
  type RequestPasswordResetBody,
} from '@/api/queries';

/** Validation messages are keys of `auth:form.errors`, translated when shown. */
const fieldErrors = [
  'required',
  'email',
  'passwordMin',
  'tooLong',
  'invalid',
  'passwordMismatch',
  'wrongPassword',
  'sameEmail',
] as const;
type FieldErrorKey = (typeof fieldErrors)[number];

const isFieldErrorKey = (key: unknown): key is FieldErrorKey =>
  fieldErrors.includes(key as FieldErrorKey);

// Limits as in the API (RegisterInput, LoginInput, ConfirmEmailInput).
const name = z.string().trim().min(1, 'required').max(200, 'tooLong');
const email = z.string().trim().min(1, 'required').max(254, 'tooLong').pipe(z.email('email'));
const password = z.string().min(1, 'required').max(256, 'tooLong');
const newPassword = password.min(8, 'passwordMin');
const repeat = z.string().min(1, 'required');

export const loginSchema = z.object({ email, password }) satisfies z.ZodType<LoginBody>;

export const registerSchema = z.object({
  givenName: name,
  familyName: name,
  email,
  password: newPassword,
}) satisfies z.ZodType<Omit<RegisterBody, 'locale'>>;

export const confirmSchema = z.object({ password }) satisfies z.ZodType<
  Omit<ConfirmEmailBody, 'token'>
>;

export const forgotPasswordSchema = z.object({ email }) satisfies z.ZodType<
  Omit<RequestPasswordResetBody, 'locale'>
>;

/** A new password, typed twice: `repeat` must equal `password`. */
export const resetPasswordSchema = z
  .object({ password: newPassword, repeat })
  .refine((v) => v.password === v.repeat, { path: ['repeat'], message: 'passwordMismatch' });

/** The current password and a new one, typed twice: `repeat` must equal `newPassword`. */
export const changePasswordSchema = z
  .object({ currentPassword: password, newPassword, repeat })
  .refine((v) => v.newPassword === v.repeat, {
    path: ['repeat'],
    message: 'passwordMismatch',
  }) satisfies z.ZodType<ChangePasswordBody & { repeat: string }>;

/** A new address for the account with address `current`, and the password. */
export const changeEmailSchema = (current: string) =>
  z.object({
    newEmail: email.refine((e) => e.toLowerCase() !== current.trim().toLowerCase(), 'sameEmail'),
    password,
  }) satisfies z.ZodType<Omit<RequestEmailChangeBody, 'locale'>>;

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

/**
 * The seconds to wait after a 429 answer: from its Retry-After header, else
 * from the problem detail ("… try again in N seconds"). Undefined for other
 * errors or when the answer does not say.
 */
export function retryAfterOf(error: unknown): number | undefined {
  if (!(error instanceof ApiError) || error.status !== 429) return undefined;
  if (error.retryAfter !== undefined) return error.retryAfter;
  const detail = (error.body as { detail?: unknown } | undefined)?.detail;
  const match = typeof detail === 'string' ? /try again in (\d+) seconds?/.exec(detail) : null;
  return match ? Number(match[1]) : undefined;
}
