import { z } from 'zod';
import type { CourseType, CourseTypeData } from '@/api/queries';
import { catalogueName } from './locationForms';

// Limits as in the API (CourseTypeData).

/** A course type's form: an empty description means none. */
export const courseTypeSchema = z.object({
  name: catalogueName,
  description: z.string().trim().max(2000, 'tooLong'),
}) satisfies z.ZodType<CourseTypeData>;

export type CourseTypeValues = z.infer<typeof courseTypeSchema>;

export const courseTypeFields = [
  'name',
  'description',
] as const satisfies (keyof CourseTypeValues)[];

export const courseTypeValues = (courseType?: CourseType): CourseTypeValues => ({
  name: courseType?.name ?? '',
  description: courseType?.description ?? '',
});

/** The request body of the form values: an empty description is left out. */
export const courseTypeData = ({ name, description }: CourseTypeValues): CourseTypeData => ({
  name,
  ...(description ? { description } : {}),
});
