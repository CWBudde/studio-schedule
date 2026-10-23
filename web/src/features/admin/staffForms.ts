import { z } from 'zod';
import type { StaffData, StaffEntry } from '@/api/queries';
import { catalogueName } from './locationForms';

// Limits as in the API (StaffData).

/** A staff member's form: an empty `personId` means no person. */
export const staffSchema = z.object({
  displayName: catalogueName,
  kind: z.enum(['instructor', 'accompanist']),
  personId: z.string(),
  active: z.boolean(),
}) satisfies z.ZodType<Omit<StaffData, 'personId'> & { personId: string }>;

export type StaffValues = z.infer<typeof staffSchema>;

export const staffFields = [
  'displayName',
  'kind',
  'personId',
  'active',
] as const satisfies (keyof StaffValues)[];

/** The form values of a staff member, or of a new, active instructor. */
export const staffValues = (staff?: StaffEntry): StaffValues => ({
  displayName: staff?.displayName ?? '',
  kind: staff?.kind ?? 'instructor',
  personId: staff?.person?.id ?? '',
  active: staff?.active ?? true,
});

/**
 * The request body of the form values: without a person, `personId` is left
 * out (the API takes an empty one for an invalid id).
 */
export const staffData = ({ personId, ...rest }: StaffValues): StaffData => ({
  ...rest,
  ...(personId ? { personId } : {}),
});
