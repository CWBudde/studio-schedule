import { z } from 'zod';
import type { LessonDetail } from '@/api/client';
import type { LessonCreateData, LessonData } from '@/api/queries';
import { chosen, durationMinutes, minutesOf, startTime } from '@/features/admin/classForms';
import { capacity } from '@/features/admin/locationForms';
import { minutesSinceMidnight, schoolDateOf } from '@/lib/schoolTime';

// Limits as in the API (LessonData, LessonCreateData, LessonCancelData).
// Messages are keys of `auth:form.errors` or `admin:form.errors` (see
// useAdminErrorText).

export const visibilities = ['public', 'members', 'hidden'] as const;
export const kinds = ['regular', 'workshop', 'private'] as const;

/**
 * A lesson's form, every field as the inputs hold it: the school date
 * 'YYYY-MM-DD' and the start time 'HH:MM' in the school's time zone, an
 * empty `accompanistId` for none. An edit keeps the lesson's course type.
 * The lesson must end on its day, and its accompanist cannot be its
 * instructor.
 */
export const lessonSchema = z
  .object({
    courseTypeId: chosen,
    date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'date'),
    startTime,
    durationMinutes,
    roomId: chosen,
    instructorId: chosen,
    accompanistId: z.string(),
    capacity,
    visibility: chosen.pipe(z.enum(visibilities)),
    kind: chosen.pipe(z.enum(kinds)),
  })
  .superRefine((v, ctx) => {
    if (v.accompanistId && v.accompanistId === v.instructorId) {
      ctx.addIssue({ code: 'custom', path: ['accompanistId'], message: 'accompanist' });
    }
    if (minutesOf(v.startTime) + v.durationMinutes > 1440) {
      ctx.addIssue({ code: 'custom', path: ['durationMinutes'], message: 'lessonMidnight' });
    }
  }) satisfies z.ZodType<
  Omit<LessonCreateData, 'accompanistId' | 'confirmRoomOverlap'> & { accompanistId: string },
  LessonInput
>;

/** The form values: what the inputs hold. */
export type LessonInput = {
  courseTypeId: string;
  date: string;
  startTime: string;
  durationMinutes: string;
  roomId: string;
  instructorId: string;
  accompanistId: string;
  capacity: string;
  visibility: string;
  kind: string;
};

export type LessonOutput = z.output<typeof lessonSchema>;

export const lessonFields = [
  'courseTypeId',
  'date',
  'startTime',
  'durationMinutes',
  'roomId',
  'instructorId',
  'accompanistId',
  'capacity',
  'visibility',
  'kind',
] as const satisfies (keyof LessonInput)[];

const pad = (n: number) => String(n).padStart(2, '0');

/** The wall-clock time 'HH:MM' of an instant in the school's time zone. */
function timeOf(instant: string, timeZone: string): string {
  const minutes = minutesSinceMidnight(instant, timeZone);
  return `${pad(Math.floor(minutes / 60))}:${pad(minutes % 60)}`;
}

/** The length of a lesson in whole minutes. */
const durationOf = (lesson: LessonDetail) =>
  Math.round((Date.parse(lesson.endsAt) - Date.parse(lesson.startsAt)) / 60_000);

/**
 * The form values of a lesson, its date and times in the school's time
 * zone; of a new one-off lesson without one: public and regular, nothing
 * else chosen yet.
 */
export function lessonValues(lesson: LessonDetail | undefined, timeZone: string): LessonInput {
  if (!lesson) {
    return {
      courseTypeId: '',
      date: '',
      startTime: '',
      durationMinutes: '',
      roomId: '',
      instructorId: '',
      accompanistId: '',
      capacity: '',
      visibility: 'public',
      kind: 'regular',
    };
  }
  return {
    courseTypeId: lesson.courseType.id,
    date: schoolDateOf(lesson.startsAt, timeZone),
    startTime: timeOf(lesson.startsAt, timeZone),
    durationMinutes: String(durationOf(lesson)),
    roomId: lesson.room.id,
    instructorId: lesson.instructor.id,
    accompanistId: lesson.accompanist?.id ?? '',
    capacity: String(lesson.capacity),
    visibility: lesson.visibility,
    kind: lesson.kind,
  };
}

/** The request body of an edit: without an accompanist, `accompanistId` is left out. */
export const lessonData = ({
  courseTypeId: _courseType,
  accompanistId,
  ...rest
}: LessonOutput): LessonData => ({ ...rest, ...(accompanistId ? { accompanistId } : {}) });

/** The request body of a new one-off lesson. */
export const newLessonData = (values: LessonOutput): LessonCreateData => ({
  courseTypeId: values.courseTypeId,
  ...lessonData(values),
});

/**
 * The request body that keeps a lesson as it is but for `changes`, e.g. a
 * hidden lesson: its date and start in the school's time zone.
 */
export const lessonDataOf = (
  lesson: LessonDetail,
  timeZone: string,
  changes: Partial<LessonData> = {},
): LessonData => ({
  date: schoolDateOf(lesson.startsAt, timeZone),
  startTime: timeOf(lesson.startsAt, timeZone),
  durationMinutes: durationOf(lesson),
  roomId: lesson.room.id,
  instructorId: lesson.instructor.id,
  ...(lesson.accompanist ? { accompanistId: lesson.accompanist.id } : {}),
  capacity: lesson.capacity,
  visibility: lesson.visibility,
  kind: lesson.kind,
  ...changes,
});

/**
 * The cancellation form: an optional note for those booked, up to 500
 * characters (code points, as the API counts) after trimming.
 */
export const cancelSchema = z.object({
  note: z
    .string()
    .trim()
    .refine((note) => [...note].length <= 500, 'tooLong'),
});

export type CancelValues = z.infer<typeof cancelSchema>;
