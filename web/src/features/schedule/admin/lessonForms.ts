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
export const kinds = ['regular', 'workshop', 'private', 'rental'] as const;

/** The longest renter of a room rental, in characters (code points, as the API counts). */
export const maxRenterLength = 100;

/** Whether a kind is a room rental (ADR 0007, amendment). */
export const isRental = (kind: string) => kind === 'rental';

/**
 * A lesson's form, every field as the inputs hold it: the school date
 * 'YYYY-MM-DD' and the start time 'HH:MM' in the school's time zone, an
 * empty `accompanistId` for none. An edit keeps the lesson's course type.
 * The lesson must end on its day, and its accompanist cannot be its
 * instructor. A room rental needs a renter instead of an instructor and a
 * capacity: its output has neither, no accompanist, a capacity of 0, and
 * it is hidden; the output of any other kind has no renter.
 */
export const lessonSchema = z
  .object({
    courseTypeId: chosen,
    date: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, 'date'),
    startTime,
    durationMinutes,
    roomId: chosen,
    instructorId: z.string(),
    accompanistId: z.string(),
    capacity: z.string(),
    visibility: chosen.pipe(z.enum(visibilities)),
    kind: chosen.pipe(z.enum(kinds)),
    renter: z.string().trim(),
  })
  .superRefine((v, ctx) => {
    if (minutesOf(v.startTime) + v.durationMinutes > 1440) {
      ctx.addIssue({ code: 'custom', path: ['durationMinutes'], message: 'lessonMidnight' });
    }
    if (isRental(v.kind)) {
      if (!v.renter) {
        ctx.addIssue({ code: 'custom', path: ['renter'], message: 'required' });
      } else if ([...v.renter].length > maxRenterLength) {
        ctx.addIssue({ code: 'custom', path: ['renter'], message: 'tooLong' });
      }
      return;
    }
    if (!v.instructorId) {
      ctx.addIssue({ code: 'custom', path: ['instructorId'], message: 'choose' });
    }
    if (!capacity.safeParse(v.capacity).success) {
      ctx.addIssue({ code: 'custom', path: ['capacity'], message: 'capacity' });
    }
    if (v.accompanistId && v.accompanistId === v.instructorId) {
      ctx.addIssue({ code: 'custom', path: ['accompanistId'], message: 'accompanist' });
    }
  })
  .transform((v) =>
    isRental(v.kind)
      ? { ...v, instructorId: '', accompanistId: '', capacity: 0, visibility: 'hidden' as const }
      : { ...v, capacity: Number(v.capacity.trim()), renter: '' },
  ) satisfies z.ZodType<
  Omit<LessonCreateData, 'instructorId' | 'accompanistId' | 'renter' | 'confirmRoomOverlap'> & {
    instructorId: string;
    accompanistId: string;
    renter: string;
  },
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
  renter: string;
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
  'renter',
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
      renter: '',
    };
  }
  return {
    courseTypeId: lesson.courseType.id,
    date: schoolDateOf(lesson.startsAt, timeZone),
    startTime: timeOf(lesson.startsAt, timeZone),
    durationMinutes: String(durationOf(lesson)),
    roomId: lesson.room.id,
    instructorId: lesson.instructor?.id ?? '',
    accompanistId: lesson.accompanist?.id ?? '',
    // A rental has no capacity: the field starts empty should it become a lesson.
    capacity: isRental(lesson.kind) ? '' : String(lesson.capacity),
    visibility: lesson.visibility,
    kind: lesson.kind,
    renter: lesson.renter ?? '',
  };
}

/**
 * The request body of an edit: an empty `instructorId`, `accompanistId` or
 * `renter` is left out.
 */
export const lessonData = ({
  courseTypeId: _courseType,
  instructorId,
  accompanistId,
  renter,
  ...rest
}: LessonOutput): LessonData => ({
  ...rest,
  ...(instructorId ? { instructorId } : {}),
  ...(accompanistId ? { accompanistId } : {}),
  ...(renter ? { renter } : {}),
});

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
  ...(lesson.instructor ? { instructorId: lesson.instructor.id } : {}),
  ...(lesson.accompanist ? { accompanistId: lesson.accompanist.id } : {}),
  capacity: lesson.capacity,
  visibility: lesson.visibility,
  kind: lesson.kind,
  ...(lesson.renter ? { renter: lesson.renter } : {}),
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
