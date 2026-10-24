import { z } from 'zod';
import type { TermData } from '@/api/queries';
import type { Term } from '@/features/timetable/timetable';
import { catalogueName } from './locationForms';

// Limits as in the API (TermData). Messages are keys of `auth:form.errors`
// or `admin:form.errors` (see useAdminErrorText).

/** A school date as a date input holds it: 'YYYY-MM-DD'. */
const date = z
  .string()
  .min(1, 'required')
  .regex(/^\d{4}-\d{2}-\d{2}$/, 'invalid');

/** A term's form: a name and its first and last day; it cannot end before it starts. */
export const termSchema = z
  .object({ name: catalogueName, startsOn: date, endsOn: date })
  .refine((v) => v.startsOn <= v.endsOn, {
    path: ['endsOn'],
    message: 'endBeforeStart',
  }) satisfies z.ZodType<TermData>;

export type TermValues = z.infer<typeof termSchema>;

export const termFields = ['name', 'startsOn', 'endsOn'] as const satisfies (keyof TermValues)[];

/** The form values of a term, or of a new one. */
export const termValues = (term?: Term): TermValues => ({
  name: term?.name ?? '',
  startsOn: term?.startsOn ?? '',
  endsOn: term?.endsOn ?? '',
});
