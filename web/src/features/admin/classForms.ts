import { z } from 'zod';
import type { ClassData, ClassEntry } from '@/api/queries';
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

/** The value of the level select for "all levels"; the other values are '' (none) and level IDs. */
export const allLevels = 'all';

/** A stage as typed: empty for none, else 1 to 9. */
export const stage = z
  .string()
  .trim()
  .refine((s) => s === '' || /^[1-9]$/.test(s), 'stage')
  .transform((s) => (s === '' ? undefined : Number(s)));

/** The months of an age in years as typed, '3.5' or '3,5'; NaN if it is no number. */
const monthsOf = (years: string) =>
  /^\d{1,3}([.,]\d+)?$/.test(years) ? Number(years.replace(',', '.')) * 12 : NaN;

/**
 * An age in years as typed: empty for none, else whole or half years from
 * ½ to 100, in months (the API's 1 to 1200, by halves as the web enters them).
 */
export const ageMonths = z
  .string()
  .trim()
  .refine((s) => {
    if (s === '') return true;
    const months = monthsOf(s);
    return months >= 6 && months <= 1200 && months % 6 === 0;
  }, 'age')
  .transform((s) => (s === '' ? undefined : monthsOf(s)));

/** The years of an age in months, as the age inputs hold them: 42 → '3.5'. */
const yearsOf = (months?: number) => (months === undefined ? '' : String(months / 12));

/**
 * A class's form, every field as the inputs hold it: an empty `accompanistId`
 * means none, `level` is '' (no level), `allLevels` or the ID of the span's
 * first level and `levelTo` the ID of its other end, the weekday is '0'
 * (Sunday) to '6', the start time 'HH:MM' in the school's time zone, the
 * ages are years. The class must end by midnight, its accompanist cannot be
 * its instructor, and its age range cannot end before it starts. The output
 * is the request body without `retired`.
 */
export const classSchema = z
  .object({
    courseTypeId: chosen,
    roomId: chosen,
    instructorId: chosen,
    accompanistId: z.string(),
    level: z.string(),
    levelTo: z.string(),
    stage,
    ageFrom: ageMonths,
    ageTo: ageMonths,
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
    if (v.ageFrom !== undefined && v.ageTo !== undefined && v.ageTo < v.ageFrom) {
      ctx.addIssue({ code: 'custom', path: ['ageTo'], message: 'ageRange' });
    }
  })
  .transform(({ accompanistId, level, levelTo, stage, ageFrom, ageTo, ...rest }): ClassBody => ({
    ...rest,
    ...(accompanistId ? { accompanistId } : {}),
    ...(level === allLevels ? { allLevels: true } : {}),
    ...(level && level !== allLevels ? { levelFromId: level, levelToId: levelTo } : {}),
    ...(stage !== undefined ? { stage } : {}),
    ...(ageFrom !== undefined ? { ageFromMonths: ageFrom } : {}),
    ...(ageTo !== undefined ? { ageToMonths: ageTo } : {}),
  })) satisfies z.ZodType<ClassBody, ClassInput>;

/** The request body of a class but for `retired`. */
type ClassBody = Omit<ClassData, 'retired'>;

/** The form values: what the inputs hold. */
export type ClassInput = {
  courseTypeId: string;
  roomId: string;
  instructorId: string;
  accompanistId: string;
  level: string;
  levelTo: string;
  stage: string;
  ageFrom: string;
  ageTo: string;
  weekday: string;
  startTime: string;
  durationMinutes: string;
  capacity: string;
};

export type ClassOutput = z.output<typeof classSchema>;

/** The form field of each request body field, for the fields of a 422 answer. */
export const classFields = {
  courseTypeId: 'courseTypeId',
  roomId: 'roomId',
  instructorId: 'instructorId',
  accompanistId: 'accompanistId',
  levelFromId: 'level',
  allLevels: 'level',
  levelToId: 'levelTo',
  stage: 'stage',
  ageFromMonths: 'ageFrom',
  ageToMonths: 'ageTo',
  weekday: 'weekday',
  startTime: 'startTime',
  durationMinutes: 'durationMinutes',
  capacity: 'capacity',
} as const satisfies Record<keyof ClassBody, keyof ClassInput>;

/** The form values of a class, or of a new one with nothing chosen yet. */
export const classValues = (c?: ClassEntry): ClassInput => ({
  courseTypeId: c?.courseTypeId ?? '',
  roomId: c?.roomId ?? '',
  instructorId: c?.instructor.id ?? '',
  accompanistId: c?.accompanist?.id ?? '',
  level: c?.allLevels ? allLevels : (c?.levelFrom?.id ?? ''),
  levelTo: c?.levelTo?.id ?? '',
  stage: c?.stage === undefined ? '' : String(c.stage),
  ageFrom: yearsOf(c?.ageFromMonths),
  ageTo: yearsOf(c?.ageToMonths),
  weekday: c ? String(c.weekday) : '',
  startTime: c?.startTime ?? '',
  durationMinutes: c ? String(c.durationMinutes) : '',
  capacity: c ? String(c.capacity) : '',
});

/** The request body of the form values. */
export const classData = (values: ClassOutput, retired: boolean): ClassData => ({
  ...values,
  retired,
});

/** The weekdays of the API (0 = Sunday) in the order of the school's week, Monday first. */
export const weekdaysMondayFirst = [1, 2, 3, 4, 5, 6, 0] as const;
