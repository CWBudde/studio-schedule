import { z } from 'zod';

const schoolDate = z.string().regex(/^\d{4}-\d{2}-\d{2}$/);

/**
 * Search params of /teach. Everything is optional so URLs stay short;
 * invalid values are dropped instead of failing the route.
 *
 * - from: first school date shown (default: a week before today, as the server has it)
 * - staff: the staff member whose lessons the office sees (default: everyone's)
 * - lesson: the lesson whose detail sheet is open (deep link)
 */
export const teachSearchSchema = z.object({
  from: schoolDate.optional().catch(undefined),
  staff: z.uuid().optional().catch(undefined),
  lesson: z.uuid().optional().catch(undefined),
});

export type TeachSearch = z.infer<typeof teachSearchSchema>;

export const validateTeachSearch = (search: Record<string, unknown>): TeachSearch =>
  teachSearchSchema.parse(search);
