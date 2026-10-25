import { z } from 'zod';
import type { AdminPerson, PersonAccountData, PersonRecord } from '@/api/queries';
import type { SchoolDate } from '@/lib/schoolTime';

// Limits as in the API (PersonRecord, PersonAccountInput). Messages are keys
// of `auth:form.errors` or `admin:form.errors` (see useAdminErrorText).

const name = z.string().trim().min(1, 'required').max(100, 'tooLong');
const isEmail = (value: string) => z.email().safeParse(value).success;

/**
 * Every field of a person as the office edits it. Empty email, phone, birth
 * date and notes mean "none". The birth date must not be after `today` (the
 * school's date); while that is unknown, the API decides.
 */
export const personSchema = (today: SchoolDate | undefined) =>
  z.object({
    givenName: name,
    familyName: name,
    email: z
      .string()
      .trim()
      .max(254, 'tooLong')
      .refine((e) => e === '' || isEmail(e), 'email'),
    phone: z.string().trim().max(50, 'tooLong'),
    birthDate: z.string().refine((d) => !today || d === '' || d <= today, 'birthDateFuture'),
    notes: z.string().trim().max(2000, 'tooLong'),
    memberNo: z.string().trim().max(40, 'tooLong'),
  });

export type PersonValues = z.infer<ReturnType<typeof personSchema>>;

export const personFields = [
  'givenName',
  'familyName',
  'email',
  'phone',
  'birthDate',
  'notes',
  'memberNo',
] as const satisfies (keyof PersonValues)[];

/** The form values of a person, or of a new one. */
export const personValues = (person?: AdminPerson): PersonValues => ({
  givenName: person?.givenName ?? '',
  familyName: person?.familyName ?? '',
  email: person?.email ?? '',
  phone: person?.phone ?? '',
  birthDate: person?.birthDate ?? '',
  notes: person?.notes ?? '',
  memberNo: person?.memberNo ?? '',
});

/** The request body of the form values: empty optional fields are left out. */
export const personRecord = ({
  givenName,
  familyName,
  email,
  phone,
  birthDate,
  notes,
  memberNo,
}: PersonValues): PersonRecord => ({
  givenName,
  familyName,
  ...(email ? { email } : {}),
  ...(phone ? { phone } : {}),
  ...(birthDate ? { birthDate } : {}),
  ...(notes ? { notes } : {}),
  ...(memberNo ? { memberNo } : {}),
});

/** The roles an account can get; admin only from an admin (the API refuses the office). */
export const accountRoles = ['member', 'instructor', 'office', 'admin'] as const;

/** A new account of a person: its email (the login name) and role. */
export const accountSchema = z.object({
  email: z.string().trim().min(1, 'required').max(254, 'tooLong').refine(isEmail, 'email'),
  role: z.enum(accountRoles),
}) satisfies z.ZodType<PersonAccountData>;

export type AccountValues = z.infer<typeof accountSchema>;

/**
 * The form that links a guardian: a person other than `personId` itself and
 * its children (`dependentIds`), which would make a cycle. Grandchildren
 * and further down are left to the API.
 */
export const guardianSchema = (personId: string, dependentIds: readonly string[]) =>
  z.object({
    guardianId: z
      .string()
      .min(1, 'choosePerson')
      .refine((id) => id !== personId, 'selfGuardian')
      .refine((id) => !dependentIds.includes(id), 'guardianCycle'),
  });

export type GuardianValues = z.infer<ReturnType<typeof guardianSchema>>;
