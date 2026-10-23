import { z } from 'zod';

const schoolDate = z.string().regex(/^\d{4}-\d{2}-\d{2}$/);
const levels = ['beginner', 'intermediate', 'advanced', 'professional', 'open'] as const;

/**
 * Search params of /schedule. Everything is optional so URLs stay short;
 * invalid values are dropped instead of failing the route.
 *
 * - view: 'list' (default) or 'week'
 * - from: first school date shown (default: today in the school time zone)
 * - location, instructor, courseType, level: filters, as in getSchedule
 * - lesson: the lesson whose detail sheet is open (deep link)
 */
export const scheduleSearchSchema = z.object({
  view: z.enum(['list', 'week']).optional().catch(undefined),
  from: schoolDate.optional().catch(undefined),
  location: z.uuid().optional().catch(undefined),
  instructor: z.uuid().optional().catch(undefined),
  courseType: z.uuid().optional().catch(undefined),
  level: z.enum(levels).optional().catch(undefined),
  lesson: z.uuid().optional().catch(undefined),
});

export type ScheduleSearch = z.infer<typeof scheduleSearchSchema>;

export const validateScheduleSearch = (search: Record<string, unknown>): ScheduleSearch =>
  scheduleSearchSchema.parse(search);
