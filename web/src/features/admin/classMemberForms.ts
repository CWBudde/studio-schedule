import { z } from 'zod';
import type { ClassMemberData } from '@/api/queries';

// Messages are keys of `auth:form.errors` or `admin:form.errors` (see
// useAdminErrorText).

/** The form that adds a member to a class: a person and, if known, since when. */
export const classMemberSchema = z.object({
  personId: z.string().min(1, 'choosePerson'),
  since: z.string().regex(/^(\d{4}-\d{2}-\d{2})?$/, 'invalid'),
});

export type ClassMemberValues = z.infer<typeof classMemberSchema>;

export const classMemberFields = [
  'personId',
  'since',
] as const satisfies (keyof ClassMemberValues)[];

export const emptyClassMember: ClassMemberValues = { personId: '', since: '' };

/** The request body of the form values: an empty `since` is left out. */
export const classMemberData = ({ personId, since }: ClassMemberValues): ClassMemberData => ({
  personId,
  ...(since ? { since } : {}),
});
