import { z } from 'zod';
import type { ClassData, ClassEntry } from '@/api/queries';
import { levels } from '@/features/schedule/search';
import { capacity } from './locationForms';

// Limits as in the API (ClassData). Messages are keys of `auth:form.errors`
// or `admin:form.errors` (see useAdminErrorText).

/** A choice of a select: an empty value means none was made. */
export const chosen = z.string().min(1, 'choose');

/** The minutes after midnight of a time of day 'HH:MM'. */
export const minutesOf = (time: string) => {
  const [h = 0, m = 0] = time.split(':').map(Number);
  return h * 60 + m;
};

/** A time of day 'HH:MM' as typed. */
export const startTime = z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, 'startTime');

/** A duration in whole minutes as typed, 1 to 1440. */
export const durationMinutes = z
  .string()
  .trim()
  .regex(/^\d+$/, 'duration')
  .transform(Number)
  .pipe(z.number().int().min(1, 'duration').max(1440, 'duration'));

/**
 * A class's form, every field as the inputs hold it: an empty `accompanistId`
 * means none, the weekday is '0' (Sunday) to '6', the start time 'HH:MM' in
 * the school's time zone. The class must end by midnight, and its
 * accompanist cannot be its instructor.
 */
export const classSchema = z
  .object({
    courseTypeId: chosen,
    roomId: chosen,
    instructorId: chosen,
    accompanistId: z.string(),
    level: chosen.pipe(z.enum(levels)),
    weekday: z
      .string()
      .regex(/^[0-6]$/, 'choose')
      .transform(Number),
    startTime,
    durationMinutes,
    capacity,
  })
  .superRefine((v, ctx) => {
    if (v.accompanistId && v.accompanistId === v.instructorId) {
      ctx.addIssue({ code: 'custom', path: ['accompanistId'], message: 'accompanist' });
    }
    if (minutesOf(v.startTime) + v.durationMinutes > 1440) {
      ctx.addIssue({ code: 'custom', path: ['durationMinutes'], message: 'midnight' });
    }
  }) satisfies z.ZodType<
  Omit<ClassData, 'retired' | 'accompanistId'> & { accompanistId: string },
  ClassInput
>;

/** The form values: what the inputs hold. */
export type ClassInput = {
  courseTypeId: string;
  roomId: string;
  instructorId: string;
  accompanistId: string;
  level: string;
  weekday: string;
  startTime: string;
  durationMinutes: string;
  capacity: string;
};

export type ClassOutput = z.output<typeof classSchema>;

export const classFields = [
  'courseTypeId',
  'roomId',
  'instructorId',
  'accompanistId',
  'level',
  'weekday',
  'startTime',
  'durationMinutes',
  'capacity',
] as const satisfies (keyof ClassInput)[];

/** The form values of a class, or of a new one with nothing chosen yet. */
export const classValues = (c?: ClassEntry): ClassInput => ({
  courseTypeId: c?.courseTypeId ?? '',
  roomId: c?.roomId ?? '',
  instructorId: c?.instructor.id ?? '',
  accompanistId: c?.accompanist?.id ?? '',
  level: c?.level ?? '',
  weekday: c ? String(c.weekday) : '',
  startTime: c?.startTime ?? '',
  durationMinutes: c ? String(c.durationMinutes) : '',
  capacity: c ? String(c.capacity) : '',
});

/** The request body of the form values: without an accompanist, `accompanistId` is left out. */
export const classData = (
  { accompanistId, ...rest }: ClassOutput,
  retired: boolean,
): ClassData => ({
  ...rest,
  ...(accompanistId ? { accompanistId } : {}),
  retired,
});

/** The weekdays of the API (0 = Sunday) in the order of the school's week, Monday first. */
export const weekdaysMondayFirst = [1, 2, 3, 4, 5, 6, 0] as const;
