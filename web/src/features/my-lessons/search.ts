import { z } from 'zod';

/**
 * Search params of /my-lessons. Invalid values are dropped instead of
 * failing the route.
 *
 * - lesson: the lesson whose detail sheet is open (deep link)
 */
export const myLessonsSearchSchema = z.object({
  lesson: z.uuid().optional().catch(undefined),
});

export type MyLessonsSearch = z.infer<typeof myLessonsSearchSchema>;

export const validateMyLessonsSearch = (search: Record<string, unknown>): MyLessonsSearch =>
  myLessonsSearchSchema.parse(search);
