import { z } from 'zod';

/**
 * Search params of /classes. An invalid value is dropped instead of failing
 * the route.
 *
 * - term: the term whose classes are shown (default: see defaultTerm)
 */
export const timetableSearchSchema = z.object({
  term: z.uuid().optional().catch(undefined),
});

export type TimetableSearch = z.infer<typeof timetableSearchSchema>;

export const validateTimetableSearch = (search: Record<string, unknown>): TimetableSearch =>
  timetableSearchSchema.parse(search);
